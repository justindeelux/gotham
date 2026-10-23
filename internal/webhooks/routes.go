package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/clientip"
	"github.com/justindeelux/gotham/internal/deploy"
)

const deliveryPathPrefix = "/api/v1/webhooks"

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type hookResponse struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	Provider      string    `json:"provider"`
	Repo          string    `json:"repo"`
	URL           string    `json:"url"`
	CreatedAt     time.Time `json:"created_at"`
}

type hookEnvelope struct {
	Hook hookResponse `json:"hook"`
}

type previewResponse struct {
	ID                   string     `json:"id"`
	ApplicationID        string     `json:"application_id"`
	PreviewApplicationID string     `json:"preview_application_id,omitempty"`
	Provider             string     `json:"provider"`
	Repo                 string     `json:"repo"`
	PRNumber             int        `json:"pr_number"`
	Branch               string     `json:"branch"`
	HeadSHA              string     `json:"head_sha"`
	Host                 string     `json:"host"`
	State                string     `json:"state"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	DeletedAt            *time.Time `json:"deleted_at,omitempty"`
}

type previewListEnvelope struct {
	Previews []previewResponse `json:"previews"`
}

type deleteEnvelope struct {
	Deleted bool `json:"deleted"`
}

type errorBody struct {
	Message string `json:"message"`
}

type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}

	r.Post("/v1/webhooks/{provider}", h.receive)

	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/applications/{id}/webhooks", h.create)
		protected.Delete("/v1/applications/{id}/webhooks", h.delete)
		if Enabled() {
			protected.Get("/v1/applications/{id}/previews", h.listPreviews)
		}
	})
}

func (h *handler) receive(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))

	delivery, err := h.svc.Receive(r.Context(), provider, r)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	status := http.StatusOK
	if delivery.Status == StatusQueued {
		status = http.StatusAccepted
	}
	writeJSON(w, status, delivery)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	hook, err := h.svc.CreateWebhook(r.Context(), userID, appID, h.svc.callbackBaseURL(r))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, hookEnvelope{Hook: newHookResponse(hook)})
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	var deleted bool
	var err error
	if forceRequested(r) {
		deleted, err = h.svc.ForgetWebhook(r.Context(), userID, appID)
	} else {
		deleted, err = h.svc.DeleteWebhook(r.Context(), userID, appID)
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteEnvelope{Deleted: deleted})
}

func forceRequested(r *http.Request) bool {
	force, err := strconv.ParseBool(strings.TrimSpace(r.URL.Query().Get("force")))
	return err == nil && force
}

func (h *handler) listPreviews(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	previews, err := h.svc.ListPreviews(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]previewResponse, 0, len(previews))
	for _, preview := range previews {
		response = append(response, newPreviewResponse(preview))
	}
	writeJSON(w, http.StatusOK, previewListEnvelope{Previews: response})
}

func (s *Service) callbackBaseURL(r *http.Request) string {
	if s.controlPlaneURL != nil {
		if base := strings.TrimSpace(s.controlPlaneURL(r.Context())); base != "" {
			return strings.TrimRight(base, "/") + deliveryPathPrefix
		}
	}
	scheme := "http"
	if clientip.IsSecure(r, s.trusted) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + deliveryPathPrefix
}

func (h *handler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.userID == nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	userID, ok := h.userID(r.Context())
	if !ok || userID == uuid.Nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	return userID, true
}

func applicationIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid application id"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBadRequest), errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: err.Error()})
	case errors.Is(err, ErrRateLimited):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: err.Error()})
	case errors.Is(err, ErrRetryable):
		h.logger.Warn("webhooks: retryable delivery failure", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "delivery can be retried"})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "insufficient team role"})
	case errors.Is(err, ErrNotConnected), errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrProvider):
		h.logger.Warn("webhooks: provider API failure", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "provider unavailable"})
	case errors.Is(err, deploy.ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "applications are disabled"})
	default:
		h.logger.Error("webhooks: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func newPreviewResponse(p Preview) previewResponse {
	response := previewResponse{
		ID:                   p.ID.String(),
		ApplicationID:        p.ApplicationID.String(),
		PreviewApplicationID: p.PreviewApplicationID.String(),
		Provider:             p.Provider,
		Repo:                 p.Repo,
		PRNumber:             p.PRNumber,
		Branch:               p.Branch,
		HeadSHA:              p.HeadSHA,
		Host:                 p.Host,
		State:                p.State,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
	}
	if !p.DeletedAt.IsZero() {
		deleted := p.DeletedAt
		response.DeletedAt = &deleted
	}
	return response
}

func newHookResponse(h Hook) hookResponse {
	return hookResponse{
		ID:            h.ID.String(),
		ApplicationID: h.ApplicationID.String(),
		Provider:      h.Provider,
		Repo:          h.Repo,
		URL:           h.URL,
		CreatedAt:     h.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
