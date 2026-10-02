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

// maxBodyBytes bounds container request bodies (pull/run options).
const maxBodyBytes = 1 << 20 // 1 MiB

// containerListEnvelope wraps a container list.
type containerListEnvelope struct {
	Containers []Container `json:"containers"`
}

// containerActionEnvelope reports the container an action targeted or created.
type containerActionEnvelope struct {
	ContainerID string `json:"container_id"`
}

// pullRequest is the body of POST .../images/pull.
type pullRequest struct {
	Image string `json:"image"`
}

// pullEnvelope echoes the pulled image.
type pullEnvelope struct {
	Image string `json:"image"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the container routes for one ContainerService.
type handler struct {
	svc    ContainerService
	logger *slog.Logger
}

// Mount registers the authenticated container endpoints on r:
//
//	GET  /v1/servers/{id}/containers
//	POST /v1/servers/{id}/containers/{containerID}/start
//	POST /v1/servers/{id}/containers/{containerID}/stop
//	POST /v1/servers/{id}/containers/{containerID}/restart
//	POST /v1/servers/{id}/images/pull
//	POST /v1/servers/{id}/containers/run
//
// auth wraps the group: the server passes its team chain (RequireAuth +
// RequireTeam + the write gate), and the service authorizes the target node's
// team and role before any agent or cache access. A nil svc is a no-op so the
// control plane can call Mount unconditionally.
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

// list serves GET /v1/servers/{id}/containers.
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

// start serves POST .../containers/{containerID}/start.
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

// stop serves POST .../containers/{containerID}/stop.
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

// restart serves POST .../containers/{containerID}/restart.
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

// pull serves POST .../images/pull.
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

// run serves POST .../containers/run.
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

// reservedLabelPrefix marks every label the control plane uses to classify a
// managed container. A raw run must not spoof one: gotham.component=proxy
// would grant access to the node's proxy directory (and its ACME keys), and
// gotham.app_id would grant a managed bind.
const reservedLabelPrefix = "gotham."

// reservedVolumePrefix marks every platform-owned Docker named volume
// (gotham-db-*, gotham-app-*, gotham-registry). A raw run must not mount one.
const reservedVolumePrefix = "gotham-"

// validateRawRun refuses a user-facing raw container-run request that spoofs a
// managed label or mounts a reserved named volume. It applies only to the HTTP
// route: the control plane's own services (proxy, databases, backups) call
// Service.Run directly and keep their labels and volumes.
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

// volumeSource returns the source side of a "source:target" (or
// "source:target:mode") mount spec.
func volumeSource(spec string) string {
	if index := strings.IndexByte(spec, ':'); index >= 0 {
		return strings.TrimSpace(spec[:index])
	}
	return strings.TrimSpace(spec)
}

// writeError maps service sentinels to HTTP responses.
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

// serverIDParam parses the {id} path parameter, answering 400 on a bad UUID.
func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

// actionParams parses the {id} and {containerID} parameters, answering 400
// when either is missing or malformed.
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

// decodeBody decodes a size-limited JSON body, answering 400 on failure.
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

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
