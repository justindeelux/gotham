package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// maxBodyBytes bounds service request bodies. A compose document is capped at
// MaxComposeYAML, so 2 MiB leaves room for the environment and JSON overhead
// while still bounding the request.
const maxBodyBytes = 2 << 20

// defaultLogTail is the number of lines a log read returns when the caller
// sends no tail (0 would stream the whole history).
const defaultLogTail = 200

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors databases.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// serviceResponse is the wire representation of a service (see
// ServiceResponse, which the projects resources surface reuses so the
// shapes cannot drift).
type serviceResponse = ServiceResponse

// deployResponse is the wire representation of one deploy attempt. The
// rendered document is omitted (it can be large and contains the values the
// owner already has).
type deployResponse struct {
	ID         string      `json:"id"`
	ServiceID  string      `json:"service_id"`
	State      DeployState `json:"state"`
	Error      string      `json:"error,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

// composeContainerResponse is the wire representation of one project
// container.
type composeContainerResponse struct {
	Service     string   `json:"service"`
	ContainerID string   `json:"container_id"`
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	State       string   `json:"state"`
	Status      string   `json:"status"`
	Health      string   `json:"health,omitempty"`
	Ports       []string `json:"ports,omitempty"`
}

// serviceEnvelope wraps a single service.
type serviceEnvelope struct {
	Service serviceResponse `json:"service"`
}

// serviceListEnvelope wraps a service list.
type serviceListEnvelope struct {
	Services []serviceResponse `json:"services"`
}

// deployEnvelope wraps a service with the attempt that just ran.
type deployEnvelope struct {
	Service serviceResponse `json:"service"`
	Deploy  deployResponse  `json:"deploy"`
}

// deployListEnvelope wraps a deploy history.
type deployListEnvelope struct {
	Deploys []deployResponse `json:"deploys"`
}

// containerListEnvelope wraps a project's containers.
type containerListEnvelope struct {
	Containers []composeContainerResponse `json:"containers"`
}

// createRequest is the body of POST /v1/services.
type createRequest struct {
	Name          string            `json:"name"`
	EnvironmentID string            `json:"environment_id"`
	ServerID      string            `json:"server_id"`
	ComposeYAML   string            `json:"compose_yaml"`
	Env           map[string]string `json:"env,omitempty"`
}

// updateRequest is the body of PATCH /v1/services/{id}. Environment and
// server moves ride string fields (UUIDs do not decode from JSON strings);
// the handler converts them into the service-level UpdateRequest.
type updateRequest struct {
	Name          *string           `json:"name,omitempty"`
	ComposeYAML   *string           `json:"compose_yaml,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	EnvironmentID *string           `json:"environment_id,omitempty"`
	ServerID      *string           `json:"server_id,omitempty"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the service routes for one ServiceService.
type handler struct {
	svc    ServiceService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated service endpoints under /api:
//
//	POST   /v1/services
//	GET    /v1/services
//	GET    /v1/services/{id}
//	PATCH  /v1/services/{id}
//	DELETE /v1/services/{id}
//	POST   /v1/services/{id}/deploy
//	POST   /v1/services/{id}/stop
//	POST   /v1/services/{id}/restart
//	GET    /v1/services/{id}/deploys
//	GET    /v1/services/{id}/containers
//	GET    /v1/services/{id}/logs
//
// auth wraps the group: the server passes its team chain plus the resource
// scope boundary (reads need read, mutations need deploy), because a service
// runs arbitrary compose on a node (bind mounts, published ports). A nil svc or
// FEATURE_SERVICES=false mounts nothing, so the control plane can call Mount
// unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc ServiceService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/services", h.create)
		protected.Get("/v1/services", h.list)
		protected.Get("/v1/services/{id}", h.get)
		protected.Patch("/v1/services/{id}", h.update)
		protected.Delete("/v1/services/{id}", h.delete)
		protected.Post("/v1/services/{id}/deploy", h.deploy)
		protected.Post("/v1/services/{id}/stop", h.stop)
		protected.Post("/v1/services/{id}/restart", h.restart)
		protected.Get("/v1/services/{id}/deploys", h.deploys)
		protected.Get("/v1/services/{id}/containers", h.containers)
		protected.Get("/v1/services/{id}/logs", h.logs)
	})
}

// create serves POST .../services: 201 with the stored service.
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if !decodeBody(w, r, &req) {
		return
	}
	environmentID, ok := parseRequiredUUID(w, req.EnvironmentID, "environment_id", "environment is required")
	if !ok {
		return
	}
	serverID, ok := parseRequiredUUID(w, req.ServerID, "server_id", "server is required")
	if !ok {
		return
	}
	service, err := h.svc.Create(r.Context(), userID, CreateRequest{
		Name:          req.Name,
		EnvironmentID: environmentID,
		ServerID:      serverID,
		ComposeYAML:   req.ComposeYAML,
		Env:           req.Env,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, serviceEnvelope{Service: h.newServiceResponse(service, true)})
}

// list serves GET .../services, optionally scoped by ?environment_id= or
// ?project_id=.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	filter, ok := serviceFilter(w, r)
	if !ok {
		return
	}
	services, err := h.svc.List(r.Context(), userID, filter)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]serviceResponse, 0, len(services))
	for _, service := range services {
		response = append(response, h.newServiceResponse(service, false))
	}
	writeJSON(w, http.StatusOK, serviceListEnvelope{Services: response})
}

// get serves GET .../services/{id}.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	service, err := h.svc.Get(r.Context(), userID, serviceID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceEnvelope{Service: h.newServiceResponse(service, true)})
}

// update serves PATCH .../services/{id}.
func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	in := UpdateRequest{
		Name:        req.Name,
		ComposeYAML: req.ComposeYAML,
		Env:         req.Env,
	}
	if req.EnvironmentID != nil {
		environmentID, err := uuid.Parse(strings.TrimSpace(*req.EnvironmentID))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid environment id"})
			return
		}
		in.EnvironmentID = &environmentID
	}
	if req.ServerID != nil {
		serverID, err := uuid.Parse(strings.TrimSpace(*req.ServerID))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
			return
		}
		in.ServerID = &serverID
	}
	service, err := h.svc.Update(r.Context(), userID, serviceID, in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceEnvelope{Service: h.newServiceResponse(service, true)})
}

// delete serves DELETE .../services/{id}: the project goes down, the row is
// soft-deleted and the named volumes stay. 204 on success.
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, serviceID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deploy serves POST .../services/{id}/deploy.
func (h *handler) deploy(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	service, deploy, err := h.svc.Deploy(r.Context(), userID, serviceID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deployEnvelope{Service: h.newServiceResponse(service, true), Deploy: newDeployResponse(deploy)})
}

// stop serves POST .../services/{id}/stop.
func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "stop")
}

// restart serves POST .../services/{id}/restart.
func (h *handler) restart(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "restart")
}

// action dispatches one lifecycle verb and answers with the updated service.
func (h *handler) action(w http.ResponseWriter, r *http.Request, verb string) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	var (
		service Service
		err     error
	)
	switch verb {
	case "stop":
		service, err = h.svc.Stop(r.Context(), userID, serviceID)
	case "restart":
		service, err = h.svc.Restart(r.Context(), userID, serviceID)
	default:
		err = ErrValidation
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceEnvelope{Service: h.newServiceResponse(service, false)})
}

// deploys serves GET .../services/{id}/deploys.
func (h *handler) deploys(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	deploys, err := h.svc.Deploys(r.Context(), userID, serviceID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]deployResponse, 0, len(deploys))
	for _, deploy := range deploys {
		response = append(response, newDeployResponse(deploy))
	}
	writeJSON(w, http.StatusOK, deployListEnvelope{Deploys: response})
}

// containers serves GET .../services/{id}/containers.
func (h *handler) containers(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	containers, err := h.svc.Containers(r.Context(), userID, serviceID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]composeContainerResponse, 0, len(containers))
	for _, container := range containers {
		response = append(response, composeContainerResponse(container))
	}
	writeJSON(w, http.StatusOK, containerListEnvelope{Containers: response})
}

// logs serves GET .../services/{id}/logs as a plain-text chunked stream, with
// ?service= selecting one compose service, ?tail= bounding the history and
// ?follow=true keeping it open. A node that refuses the stream is answered
// with its error status before any body byte is written; a failure after the
// stream started closes the body and is logged (the status is already sent).
func (h *handler) logs(w http.ResponseWriter, r *http.Request) {
	userID, serviceID, ok := h.serviceParams(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	tail := int64(defaultLogTail)
	if raw := strings.TrimSpace(query.Get("tail")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid tail"})
			return
		}
		tail = parsed
	}
	follow := strings.EqualFold(strings.TrimSpace(query.Get("follow")), "true")

	stream, err := h.svc.Logs(r.Context(), userID, serviceID, query.Get("service"), tail, follow)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	defer func() { _ = stream.Close() }()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// Commit the accepted response before waiting for output: a following
	// stream may legitimately stay quiet for a long time, and an HTTP client
	// must see the headers as soon as the node accepted the stream. A
	// refused/unknown selector was already answered with its error status
	// above, before this point.
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	for chunk := range stream.Chunks() {
		if _, err := w.Write(chunk); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := stream.Err(); err != nil {
		h.logger.Warn("services: log stream failed after the response started",
			"service_id", serviceID.String(), "error", err)
	}
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

// serviceParams resolves the authenticated user and the {id} path parameter,
// answering 401/400 as needed.
func (h *handler) serviceParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid service id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, serviceID, true
}

// parseRequiredUUID parses a required UUID field: empty answers 400 with
// the required message, anything else must parse (400 naming the field).
func parseRequiredUUID(w http.ResponseWriter, raw, field, requiredMessage string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: requiredMessage})
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + strings.ReplaceAll(field, "_", " ")})
		return uuid.Nil, false
	}
	return id, true
}

// serviceFilter parses the ?environment_id= and ?project_id= list filters.
// At most one may be present; a malformed UUID is a 400.
func serviceFilter(w http.ResponseWriter, r *http.Request) (ServiceFilter, bool) {
	var filter ServiceFilter
	if raw := strings.TrimSpace(r.URL.Query().Get("environment_id")); raw != "" {
		envID, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid environment id"})
			return ServiceFilter{}, false
		}
		filter.EnvironmentID = envID
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		projectID, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid project id"})
			return ServiceFilter{}, false
		}
		filter.ProjectID = projectID
	}
	return filter, true
}

// writeServiceError maps service sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, teams.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "insufficient team role"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a service with that name already exists"})
	case errors.Is(err, ErrDeployInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deploy is in progress"})
	case errors.Is(err, ErrServerPinned):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deployed service cannot change server"})
	case errors.Is(err, ErrNameConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "services are disabled"})
	case errors.Is(err, ErrAgentUnavailable), errors.Is(err, ErrDeployFailed):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: err.Error()})
	default:
		h.logger.Error("services: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// newServiceResponse maps a domain service to its wire representation (see
// NewServiceResponse).
func (h *handler) newServiceResponse(service Service, withCompose bool) serviceResponse {
	return NewServiceResponse(service, withCompose)
}

// newDeployResponse maps a deploy row to its wire representation.
func newDeployResponse(deploy Deploy) deployResponse {
	response := deployResponse{
		ID:        deploy.ID.String(),
		ServiceID: deploy.ServiceID.String(),
		State:     deploy.State,
		Error:     deploy.Error,
		CreatedAt: deploy.CreatedAt,
		UpdatedAt: deploy.UpdatedAt,
	}
	if !deploy.FinishedAt.IsZero() {
		finished := deploy.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

// decodeBody decodes a required JSON body into dst. An empty or malformed
// body answers 400.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
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

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
