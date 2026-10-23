package containers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

type containerListEnvelope struct {
	Containers []Container `json:"containers"`
}

type containerActionEnvelope struct {
	ContainerID string `json:"container_id"`
}

type pullRequest struct {
	Image string `json:"image"`
}

type pullEnvelope struct {
	Image string `json:"image"`
}

type errorBody struct {
	Message string `json:"message"`
}

type handler struct {
	svc    ContainerService
	logger *slog.Logger
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc ContainerService) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/servers/{id}/containers", h.list)
		protected.Post("/v1/servers/{id}/containers/{containerID}/start", h.start)
		protected.Post("/v1/servers/{id}/containers/{containerID}/stop", h.stop)
		protected.Post("/v1/servers/{id}/containers/{containerID}/restart", h.restart)
		protected.Post("/v1/servers/{id}/images/pull", h.pull)
		protected.Post("/v1/servers/{id}/containers/run", h.run)
	})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	containers, err := h.svc.List(r.Context(), serverID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if containers == nil {
		containers = []Container{}
	}
	writeJSON(w, http.StatusOK, containerListEnvelope{Containers: containers})
}

func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	serverID, containerID, ok := actionParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.Start(r.Context(), serverID, containerID); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, containerActionEnvelope{ContainerID: containerID})
}

func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	serverID, containerID, ok := actionParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.Stop(r.Context(), serverID, containerID); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, containerActionEnvelope{ContainerID: containerID})
}

func (h *handler) restart(w http.ResponseWriter, r *http.Request) {
	serverID, containerID, ok := actionParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.Restart(r.Context(), serverID, containerID); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, containerActionEnvelope{ContainerID: containerID})
}

func (h *handler) pull(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req pullRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := h.svc.Pull(r.Context(), serverID, req.Image); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, pullEnvelope(req))
}

func (h *handler) run(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var opts RunOptions
	if !decodeBody(w, r, &opts) {
		return
	}
	if err := validateRawRun(opts); err != nil {
		h.writeError(w, err)
		return
	}
	id, err := h.svc.Run(r.Context(), serverID, opts)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, containerActionEnvelope{ContainerID: id})
}

const reservedLabelPrefix = "gotham."

const reservedVolumePrefix = "gotham-"

func validateRawRun(opts RunOptions) error {
	for key := range opts.Labels {
		if strings.HasPrefix(strings.TrimSpace(key), reservedLabelPrefix) {
			return fmt.Errorf("%w: label %q is reserved", ErrValidation, key)
		}
	}
	for _, spec := range opts.Volumes {
		if source := volumeSource(spec); strings.HasPrefix(source, reservedVolumePrefix) {
			return fmt.Errorf("%w: volume %q is reserved", ErrValidation, source)
		}
	}
	return nil
}

func volumeSource(spec string) string {
	if index := strings.IndexByte(spec, ':'); index >= 0 {
		return strings.TrimSpace(spec[:index])
	}
	return strings.TrimSpace(spec)
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrServerNotFound), errors.Is(err, ErrContainerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "insufficient team role"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "agent unavailable"})
	default:
		h.logger.Error("containers: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

func actionParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return uuid.Nil, "", false
	}
	containerID := chi.URLParam(r, "containerID")
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "container id is required"})
		return uuid.Nil, "", false
	}
	return serverID, containerID, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
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
