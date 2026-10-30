package containers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type volumeListEnvelope struct {
	Volumes []OrphanedVolume `json:"volumes"`
}

type volumeHandler struct {
	svc    VolumeService
	logger *slog.Logger
}

// MountVolumes mounts the orphaned-volume view. It is mounted separately from
// Mount because the routes are owner-only: a member can see containers but
// must not delete node storage.
func MountVolumes(r chi.Router, admin func(http.Handler) http.Handler, svc VolumeService) {
	if svc == nil || admin == nil {
		return
	}
	h := &volumeHandler{svc: svc, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(admin)
		protected.Get("/v1/servers/{id}/orphaned-volumes", h.list)
		protected.Delete("/v1/servers/{id}/orphaned-volumes/{volumeName}", h.remove)
	})
}

func (h *volumeHandler) list(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	volumes, err := h.svc.ListOrphanedVolumes(r.Context(), serverID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if volumes == nil {
		volumes = []OrphanedVolume{}
	}
	writeJSON(w, http.StatusOK, volumeListEnvelope{Volumes: volumes})
}

func (h *volumeHandler) remove(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "volumeName")
	if err := h.svc.RemoveOrphanedVolume(r.Context(), serverID, name); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *volumeHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: err.Error()})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrVolumeInUse):
		writeJSON(w, http.StatusConflict, errorBody{Message: "volume is in use by a container; stop or remove that container first"})
	case errors.Is(err, ErrVolumeListUnsupported):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "the node agent does not list volumes; update the node agent"})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "agent unavailable"})
	default:
		h.logger.Error("containers: volume request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}
