package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

type UserIDFunc func(context.Context) (uuid.UUID, bool)

type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the owner-only monitoring settings and node status routes.
// Host incidents are owner-visible only, matching the server registry.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/monitoring/settings", h.getSettings)
		protected.Put("/v1/monitoring/settings", h.saveSettings)
		protected.Delete("/v1/monitoring/settings", h.resetSettings)
		protected.Get("/v1/monitoring/nodes/{id}/status", h.nodeStatus)
	})
}

type settingsEnvelope struct {
	Settings Settings `json:"settings"`
}

type statusEnvelope struct {
	Status NodeStatus `json:"status"`
}

type saveSettingsRequest struct {
	NodeID string `json:"node_id"`
	Rules  *Rules `json:"rules"`
}

func (h *handler) getSettings(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := parseNodeID(w, r.URL.Query().Get("node_id"))
	if !ok {
		return
	}
	settings, err := h.svc.GetSettings(r.Context(), nodeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsEnvelope{Settings: settings})
}

func (h *handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req saveSettingsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	nodeID, ok := parseNodeID(w, req.NodeID)
	if !ok {
		return
	}
	if req.Rules == nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "rules are required"})
		return
	}
	settings, err := h.svc.SaveSettings(r.Context(), userID, nodeID, *req.Rules)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsEnvelope{Settings: settings})
}

func (h *handler) resetSettings(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := parseNodeID(w, r.URL.Query().Get("node_id"))
	if !ok {
		return
	}
	if err := h.svc.ResetSettings(r.Context(), nodeID); err != nil {
		h.writeError(w, err)
		return
	}
	settings, err := h.svc.GetSettings(r.Context(), nodeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsEnvelope{Settings: settings})
}

func (h *handler) nodeStatus(w http.ResponseWriter, r *http.Request) {
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid node id"})
		return
	}
	status, err := h.svc.NodeStatus(r.Context(), nodeID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusEnvelope{Status: status})
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

func parseNodeID(w http.ResponseWriter, raw string) (*uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid node id"})
		return nil, false
	}
	return &id, true
}

type errorBody struct {
	Message string `json:"message"`
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	default:
		h.logger.Error("monitoring: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "request body is required"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
