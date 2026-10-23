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
	Errors  FieldErrors `json:"errors,omitempty"`
	Log     string      `json:"log,omitempty"`
}

type stateEnvelope struct {
	Settings State `json:"settings"`
}

type hostHelperEnvelope struct {
	Settings State  `json:"settings"`
	Log      string `json:"log"`
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
		g.Put("/v1/instance/settings/system", h.putSystem)
		g.Put("/v1/instance/settings/network", h.putNetwork)
		g.Post("/v1/instance/settings/network/confirm", h.confirm)
		g.Post("/v1/instance/settings/network/revert", h.revert)
		g.Post("/v1/instance/settings/host-helper", h.installHostHelper)
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

func (h *handler) putSystem(w http.ResponseWriter, r *http.Request) {
	var in System
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateSystem(r.Context(), h.actor(r), in)
	h.reply(w, st, err)
}

func (h *handler) putNetwork(w http.ResponseWriter, r *http.Request) {
	var in NetworkInput
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateNetwork(r.Context(), h.actor(r), in)
	h.reply(w, st, err)
}

func (h *handler) confirm(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.ConfirmNetwork(r.Context(), h.actor(r))
	h.reply(w, st, err)
}

func (h *handler) revert(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.RevertNetwork(r.Context(), h.actor(r))
	h.reply(w, st, err)
}

func (h *handler) installHostHelper(w http.ResponseWriter, r *http.Request) {
	st, log, err := h.svc.InstallHostHelper(r.Context(), h.actor(r))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, hostHelperEnvelope{Settings: st, Log: log})
	case errors.Is(err, ErrUnsupported):
		writeJSON(w, http.StatusConflict, errorBody{Message: "this host does not support applying these settings from Gotham"})
	default:
		h.logger.Error("instance host helper install failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{
			Message: "the host helper could not be installed",
			Log:     log,
		})
	}
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
	case errors.Is(err, ErrUnsupported):
		writeJSON(w, http.StatusConflict, errorBody{Message: "this host does not support applying these settings from Gotham"})
	case errors.Is(err, ErrDomainPending), errors.Is(err, ErrNoDomainPending), errors.Is(err, ErrDomainNotSynced), errors.Is(err, ErrConfirmDomainHost), errors.Is(err, ErrPending), errors.Is(err, ErrNoPending), errors.Is(err, ErrRiskyNetwork):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrHost):
		h.logger.Error("instance host helper failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "the host helper failed; the change was not applied"})
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
