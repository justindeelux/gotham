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

// deliveryPathPrefix is the public path the Git host posts to, relative to the
// control plane's origin. It mirrors where Mount registers the route once the
// server has mounted this package under /api.
const deliveryPathPrefix = "/api/v1/webhooks"

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors the deploy and providers packages for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// hookResponse is the wire representation of an installed hook. The signing
// secret never appears here.
type hookResponse struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	Provider      string    `json:"provider"`
	Repo          string    `json:"repo"`
	URL           string    `json:"url"`
	CreatedAt     time.Time `json:"created_at"`
}

// hookEnvelope wraps a single hook.
type hookEnvelope struct {
	Hook hookResponse `json:"hook"`
}

// previewResponse is the wire representation of one preview binding. It
// carries no secrets: the sibling application is referenced by ID, and the
// branch/host are the operator-facing facts of the preview.
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

// previewListEnvelope wraps an application's previews.
type previewListEnvelope struct {
	Previews []previewResponse `json:"previews"`
}

// deleteEnvelope reports an idempotent delete.
type deleteEnvelope struct {
	Deleted bool `json:"deleted"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the webhook routes for one Service.
type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the webhook endpoints on r:
//
//	POST   /v1/webhooks/{provider}          (public: signature-verified)
//	POST   /v1/applications/{id}/webhooks   (authenticated, idempotent)
//	DELETE /v1/applications/{id}/webhooks   (authenticated, idempotent;
//	                                         ?force=true forgets the stored
//	                                         hook when the host is gone)
//	GET    /v1/applications/{id}/previews   (authenticated; previews enabled)
//
// auth wraps the management group: the server passes its team chain
// (RequireAuth + RequireTeam + the write gate), and the service additionally
// authorizes the parent application's team and role, so a creator demoted to
// read_only or removed from the team can no longer install or remove a hook.
// The delivery route deliberately has no auth middleware: it authenticates
// each delivery with the per-hook secret instead of a session. A nil svc
// mounts nothing, so the control plane can call Mount unconditionally.
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
			// The preview surface does not exist with FEATURE_PREVIEWS=false:
			// no listing route, no delivery handling, no sweep.
			protected.Get("/v1/applications/{id}/previews", h.listPreviews)
		}
	})
}

// receive serves POST /v1/webhooks/{provider}: verify the delivery, then queue
// a deployment for the application it belongs to.
func (h *handler) receive(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))

	delivery, err := h.svc.Receive(r.Context(), provider, r)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	// Only a queued deployment is a 202: everything else is a completed
	// no-op that the provider's delivery log should record as delivered.
	status := http.StatusOK
	if delivery.Status == StatusQueued {
		status = http.StatusAccepted
	}
	writeJSON(w, status, delivery)
}

// create serves POST .../webhooks: install the push hook of an application.
// Repeating the call returns the hook that is already installed.
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

// delete serves DELETE .../webhooks: remove the hook of an application. An
// application without a hook answers 200 with deleted=false. ?force=true
// forgets the stored hook WITHOUT contacting the Git host — the documented
// escape hatch for deleting an application whose provider connection is gone
// or stalling; the remote hook is then left behind and only the warning
// records it. Every other value keeps the strict, host-first semantics.
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

// forceRequested reports whether the caller asked for the force escape hatch
// (?force=true). Any other value (including an unparsable one) keeps the
// strict semantics, so a typo can never silently orphan a remote hook.
func forceRequested(r *http.Request) bool {
	force, err := strconv.ParseBool(strings.TrimSpace(r.URL.Query().Get("force")))
	return err == nil && force
}

// listPreviews serves GET /v1/applications/{id}/previews: the application's
// preview bindings, newest first, for callers whose team may read the
// application.
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

// callbackBaseURL derives the public origin of the delivery route: the
// instance control-plane URL when it is set, otherwise the management
// request (scheme from the trusted proxy's X-Forwarded-Proto or direct TLS,
// host from the request). Forwarded scheme headers from an untrusted peer
// are ignored, so a client cannot redirect the callback to http. A proxy
// that drops X-Forwarded-Proto yields http, which the host then rejects
// loudly instead of silently mis-routing deliveries.
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

// currentUser resolves the authenticated user, answering 401 when absent.
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

// applicationIDParam parses the {id} path parameter, answering 400 on a bad
// UUID.
func applicationIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid application id"})
		return uuid.Nil, false
	}
	return id, true
}

// writeServiceError maps service sentinels to HTTP responses. The 401 of a
// delivery must not distinguish "unknown repository" from "bad signature".
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBadRequest), errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrUnauthorized):
		// Both "no hook watches this repository" and "signature does not
		// verify" answer alike, so a probe cannot tell them apart.
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: err.Error()})
	case errors.Is(err, ErrRateLimited):
		writeJSON(w, http.StatusTooManyRequests, errorBody{Message: err.Error()})
	case errors.Is(err, ErrRetryable):
		// 503: the delivery was not acted on (a conflicting deployment is
		// running, a duplicate's winning claim is not yet durable, or a
		// transient dependency failed). The message names the condition without
		// leaking provider or database detail, and the Git host can redeliver
		// the same event.
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

// newPreviewResponse maps a stored preview binding to its wire form.
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

// newHookResponse maps a stored hook to its wire representation.
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

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
