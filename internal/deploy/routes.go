package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxBodyBytes bounds deploy request bodies (the rollback body is tiny).
const maxBodyBytes = 1 << 20 // 1 MiB

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors providers.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// deploymentResponse is the wire representation of a deployment. Sealed
// secrets never appear here — only image references, state and error text.
type deploymentResponse struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	Kind          Kind       `json:"kind"`
	State         State      `json:"state"`
	ImageTag      string     `json:"image_tag,omitempty"`
	RegistryImage string     `json:"registry_image,omitempty"`
	Digest        string     `json:"digest,omitempty"`
	Error         string     `json:"error,omitempty"`
	Attempt       int32      `json:"attempt"`
	ContainerID   string     `json:"container_id,omitempty"`
	RollbackFrom  string     `json:"rollback_from,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// deploymentEnvelope wraps a single deployment.
type deploymentEnvelope struct {
	Deployment deploymentResponse `json:"deployment"`
}

// deploymentListEnvelope wraps a deployment list.
type deploymentListEnvelope struct {
	Deployments []deploymentResponse `json:"deployments"`
}

// rollbackRequest is the optional body of POST .../rollback. With no body the
// server picks the previous successful release.
type rollbackRequest struct {
	DeploymentID string `json:"deployment_id"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the application deploy routes for one DeployService.
type handler struct {
	svc    DeployService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated application endpoints on r:
//
//	POST /v1/applications/{id}/deploy
//	GET  /v1/applications/{id}/deployments
//	POST /v1/applications/{id}/rollback
//
// auth wraps the group (the server passes its RequireAuth); a nil svc or
// FEATURE_APPLICATIONS=false mounts nothing, so the control plane can call
// Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc DeployService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/applications/{id}/deploy", h.deploy)
		protected.Get("/v1/applications/{id}/deployments", h.list)
		protected.Post("/v1/applications/{id}/rollback", h.rollback)
	})
}

// deploy serves POST .../deploy: validates, persists a queued deployment and
// answers as soon as the job is queued (202) — the state machine runs on the
// worker pool and streams its progress on the deployment's log channel.
func (h *handler) deploy(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	deployment, err := h.svc.Deploy(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deploymentEnvelope{Deployment: newDeploymentResponse(deployment)})
}

// list serves GET .../deployments.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	deployments, err := h.svc.ListDeployments(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]deploymentResponse, 0, len(deployments))
	for _, deployment := range deployments {
		response = append(response, newDeploymentResponse(deployment))
	}
	writeJSON(w, http.StatusOK, deploymentListEnvelope{Deployments: response})
}

// rollback serves POST .../rollback with an optional deployment_id body.
func (h *handler) rollback(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	appID, ok := applicationIDParam(w, r)
	if !ok {
		return
	}

	var req rollbackRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	var target uuid.UUID
	if strings.TrimSpace(req.DeploymentID) != "" {
		parsed, err := uuid.Parse(req.DeploymentID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid deployment id"})
			return
		}
		target = parsed
	}

	deployment, err := h.svc.Rollback(r.Context(), userID, appID, target)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, deploymentEnvelope{Deployment: newDeploymentResponse(deployment)})
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

// writeServiceError maps service sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deployment is already in progress"})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "applications are disabled"})
	case errors.Is(err, ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: "agent unavailable"})
	default:
		h.logger.Error("deploy: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
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

// decodeOptionalBody decodes a JSON body when one is present; an empty body
// leaves dst untouched. Failures answer 400.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

// newDeploymentResponse maps a domain deployment to its wire representation.
func newDeploymentResponse(d Deployment) deploymentResponse {
	response := deploymentResponse{
		ID:            d.ID.String(),
		ApplicationID: d.ApplicationID.String(),
		Kind:          d.Kind,
		State:         d.State,
		ImageTag:      d.ImageTag,
		RegistryImage: d.RegistryImage,
		Digest:        d.Digest,
		Error:         d.Error,
		Attempt:       d.Attempt,
		ContainerID:   d.ContainerID,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
	if d.RollbackFrom != uuid.Nil {
		response.RollbackFrom = d.RollbackFrom.String()
	}
	if !d.StartedAt.IsZero() {
		started := d.StartedAt
		response.StartedAt = &started
	}
	if !d.FinishedAt.IsZero() {
		finished := d.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
