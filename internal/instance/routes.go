package instance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type errorBody struct {
	Message string      `json:"message"`
	Code    string      `json:"code,omitempty"`
	Errors  FieldErrors `json:"errors,omitempty"`
	Log     string      `json:"log,omitempty"`
}

type stateEnvelope struct {
	Settings State `json:"settings"`
}

type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

func Mount(r chi.Router, admin func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(g chi.Router) {
		g.Use(admin)
		g.Get("/v1/instance/settings", h.get)
		g.Put("/v1/instance/settings/general", h.putGeneral)
		g.Put("/v1/instance/settings/cleanup", h.putCleanup)
		g.Post("/v1/instance/settings/control-plane-url/confirm", h.confirmControlPlaneURL)
		g.Post("/v1/instance/settings/control-plane-url/revert", h.revertControlPlaneURL)
	})
}

func (h *handler) actor(r *http.Request) uuid.UUID {
	id, _ := h.userID(r.Context())
	return id
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Get(r.Context())
	h.reply(w, st, err)
}

func (h *handler) putGeneral(w http.ResponseWriter, r *http.Request) {
	var in GeneralInput
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateGeneral(r.Context(), h.actor(r), in)
	h.reply(w, st, err)
}

func (h *handler) putCleanup(w http.ResponseWriter, r *http.Request) {
	var in RetentionInput
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateCleanupRetention(r.Context(), h.actor(r), in)
	h.reply(w, st, err)
}

func (h *handler) confirmControlPlaneURL(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.ConfirmControlPlaneURL(r.Context(), h.actor(r), r.Host)
	h.reply(w, st, err)
}

func (h *handler) revertControlPlaneURL(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.RevertControlPlaneURL(r.Context(), h.actor(r))
	h.reply(w, st, err)
}

func (h *handler) reply(w http.ResponseWriter, st State, err error) {
	var fe FieldErrors
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, stateEnvelope{Settings: st})
	case errors.As(err, &fe):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "validation failed", Errors: fe})
	case errors.Is(err, ErrDomainPending), errors.Is(err, ErrNoDomainPending), errors.Is(err, ErrDomainNotSynced), errors.Is(err, ErrConfirmDomainHost):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	default:
		h.logger.Error("instance settings", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
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
