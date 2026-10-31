package instance

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc/status"
)

type nodeEnvelope struct {
	Host NodeState `json:"host"`
}

type nodeHelperEnvelope struct {
	Host NodeState `json:"host"`
	Log  string    `json:"log,omitempty"`
}

type nodeHandler struct {
	svc    *NodeService
	userID UserIDFunc
	logger *slog.Logger
}

// MountNode mounts the per-server host settings. The gate is owner-only, the
// same as the instance settings it replaces.
func MountNode(r chi.Router, admin func(http.Handler) http.Handler, userID UserIDFunc, svc *NodeService) {
	if svc == nil {
		return
	}
	h := &nodeHandler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(g chi.Router) {
		g.Use(admin)
		g.Get("/v1/servers/{id}/host", h.get)
		g.Put("/v1/servers/{id}/host/system", h.putSystem)
		g.Put("/v1/servers/{id}/host/network", h.putNetwork)
		g.Post("/v1/servers/{id}/host/network/confirm", h.confirm)
		g.Post("/v1/servers/{id}/host/network/revert", h.revert)
		g.Put("/v1/servers/{id}/host/ssh", h.putSSH)
		g.Put("/v1/servers/{id}/host/agent-root", h.putAgentRoot)
		g.Post("/v1/servers/{id}/host/helper", h.installHelper)
	})
}

func (h *nodeHandler) actor(r *http.Request) uuid.UUID {
	id, _ := h.userID(r.Context())
	return id
}

func (h *nodeHandler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	st, err := h.svc.Get(r.Context(), id)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) putSystem(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var in System
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateSystem(r.Context(), h.actor(r), id, in)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) putNetwork(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var in NetworkInput
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateNetwork(r.Context(), h.actor(r), id, in)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) putSSH(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var in SSHInput
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateSSH(r.Context(), h.actor(r), id, in)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) putAgentRoot(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := h.svc.UpdateAgentRoot(r.Context(), h.actor(r), id, in.Enabled)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) confirm(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	st, err := h.svc.ConfirmNetwork(r.Context(), h.actor(r), id)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) revert(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	st, err := h.svc.RevertNetwork(r.Context(), h.actor(r), id)
	h.reply(w, st, "", err)
}

func (h *nodeHandler) installHelper(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	st, log, err := h.svc.InstallHelper(r.Context(), h.actor(r), id)
	if err == nil {
		writeJSON(w, http.StatusOK, nodeHelperEnvelope{Host: st, Log: log})
		return
	}
	h.reply(w, st, log, err)
}

func (h *nodeHandler) reply(w http.ResponseWriter, st NodeState, log string, err error) {
	var fe FieldErrors
	var unsupported *UnsupportedError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, nodeEnvelope{Host: st})
	case errors.Is(err, ErrNodeNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "server not found"})
	case errors.As(err, &fe):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "validation failed", Errors: fe})
	case errors.As(err, &unsupported):
		writeJSON(w, http.StatusConflict, errorBody{Message: unsupported.Error()})
	case errors.Is(err, ErrAgentRestarting):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error(), Code: "agent_restarting"})
	case errors.Is(err, ErrPending), errors.Is(err, ErrNoPending), errors.Is(err, ErrRiskyNetwork),
		errors.Is(err, ErrUnsupported), errors.Is(err, ErrAgentOutdated), errors.Is(err, ErrNodeOffline):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrHost):
		h.logger.Error("node host helper failed", "error", err)
		if log == "" {
			log = helperReason(err)
		}
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "the host helper failed; the change was not applied", Log: log})
	default:
		h.logger.Error("node host settings", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// helperReason returns the helper's own diagnostic for the owner: the local
// sudo path carries it after the ErrHost wrapper, an agent node carries it in
// the gRPC status message. Raw transport text is never shown.
func helperReason(err error) string {
	msg := strings.TrimSpace(err.Error())
	if prefix := ErrHost.Error() + ": "; strings.HasPrefix(msg, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(msg, prefix))
	}
	// errors.As finds the *status.Error inside errors.Join without letting
	// status.FromError replace its message with the full wrapped text.
	var grpcErr interface{ GRPCStatus() *status.Status }
	if errors.As(err, &grpcErr) {
		return strings.TrimSpace(grpcErr.GRPCStatus().Message())
	}
	return ""
}

func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}
