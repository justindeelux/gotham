package databases

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

	"github.com/justindeelux/gotham/internal/runtimewatch"
)

const maxBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type databaseResponse struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	EnvironmentID   string               `json:"environment_id"`
	EnvironmentName string               `json:"environment_name"`
	ProjectID       string               `json:"project_id"`
	ProjectName     string               `json:"project_name"`
	Engine          string               `json:"engine"`
	Version         string               `json:"version,omitempty"`
	Status          Status               `json:"status"`
	StatusMessage   string               `json:"status_message,omitempty"`
	ServerID        string               `json:"server_id"`
	ServerName      string               `json:"server_name"`
	ContainerID     string               `json:"container_id,omitempty"`
	PublicPort      int32                `json:"public_port"`
	Volume          string               `json:"volume"`
	RuntimeHealth   *runtimewatch.Health `json:"runtime_health,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

type databaseEnvelope struct {
	Database databaseResponse `json:"database"`
}

type databaseListEnvelope struct {
	Databases []databaseResponse `json:"databases"`
}

type createDatabaseEnvelope struct {
	Database    databaseResponse `json:"database"`
	Credentials Credentials      `json:"credentials"`
}

type credentialsEnvelope struct {
	Credentials Credentials `json:"credentials"`
}

type createRequest struct {
	Name          string `json:"name"`
	Engine        string `json:"engine"`
	Version       string `json:"version,omitempty"`
	EnvironmentID string `json:"environment_id"`
	ServerID      string `json:"server_id"`
	PublicPort    int32  `json:"public_port,omitempty"`
}

type updateRequest struct {
	Name           *string `json:"name,omitempty"`
	EnvironmentID  *string `json:"environment_id,omitempty"`
	ServerID       *string `json:"server_id,omitempty"`
	MonitorEnabled *bool   `json:"monitor_enabled,omitempty"`
	MonitorGrace   *int    `json:"monitor_grace_seconds,omitempty"`
}

type errorBody struct {
	Message string `json:"message"`
}

type handler struct {
	svc    DatabaseService
	userID UserIDFunc
	logger *slog.Logger
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, deployScope func(http.Handler) http.Handler, userID UserIDFunc, svc DatabaseService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/databases", h.create)
		protected.Get("/v1/databases", h.list)
		protected.Get("/v1/databases/{id}", h.get)
		protected.Patch("/v1/databases/{id}", h.update)
		protected.Delete("/v1/databases/{id}", h.delete)
		protected.With(deployScope).Get("/v1/databases/{id}/credentials", h.credentials)
		protected.Post("/v1/databases/{id}/start", h.start)
		protected.Post("/v1/databases/{id}/stop", h.stop)
		protected.Post("/v1/databases/{id}/restart", h.restart)
	})
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if !decodeBody(w, r, &req) {
		return
	}
	serverID, ok := parseRequiredUUID(w, req.ServerID, "server_id", "server is required")
	if !ok {
		return
	}
	environmentID, ok := parseRequiredUUID(w, req.EnvironmentID, "environment_id", "environment is required")
	if !ok {
		return
	}

	database, credentials, err := h.svc.Create(r.Context(), userID, CreateRequest{
		Name:          req.Name,
		Engine:        req.Engine,
		Version:       req.Version,
		EnvironmentID: environmentID,
		ServerID:      serverID,
		PublicPort:    req.PublicPort,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, createDatabaseEnvelope{
		Database:    newDatabaseResponse(database),
		Credentials: credentials,
	})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	filter, ok := databaseFilter(w, r)
	if !ok {
		return
	}
	databases, err := h.svc.List(r.Context(), userID, filter)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]databaseResponse, 0, len(databases))
	for _, database := range databases {
		response = append(response, newDatabaseResponse(database))
	}
	writeJSON(w, http.StatusOK, databaseListEnvelope{Databases: response})
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	database, err := h.svc.Get(r.Context(), userID, databaseID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, databaseEnvelope{Database: newDatabaseResponse(database)})
}

func (h *handler) credentials(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	credentials, err := h.svc.Credentials(r.Context(), userID, databaseID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialsEnvelope{Credentials: credentials})
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	in := UpdateRequest{
		Name:           req.Name,
		MonitorEnabled: req.MonitorEnabled,
		MonitorGrace:   req.MonitorGrace,
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
	database, err := h.svc.Update(r.Context(), userID, databaseID, in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, databaseEnvelope{Database: newDatabaseResponse(database)})
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, databaseID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "start")
}

func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "stop")
}

func (h *handler) restart(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "restart")
}

func (h *handler) action(w http.ResponseWriter, r *http.Request, verb string) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}

	var (
		database Database
		err      error
	)
	switch verb {
	case "start":
		database, err = h.svc.Start(r.Context(), userID, databaseID)
	case "stop":
		database, err = h.svc.Stop(r.Context(), userID, databaseID)
	case "restart":
		database, err = h.svc.Restart(r.Context(), userID, databaseID)
	default:
		err = ErrValidation
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, databaseEnvelope{Database: newDatabaseResponse(database)})
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

func (h *handler) databaseParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	databaseID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid database id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, databaseID, true
}

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

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a database with that name already exists"})
	case errors.Is(err, ErrDeployInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a deploy is in progress"})
	case errors.Is(err, ErrServerPinned):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a database cannot change server once created"})
	case errors.Is(err, ErrNameConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrPortConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "the requested public port is already in use"})
	case errors.Is(err, ErrDatabaseBusy):
		writeJSON(w, http.StatusConflict, errorBody{Message: "another operation is running for this database"})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "databases are disabled"})
	case errors.Is(err, ErrAgentUnavailable), errors.Is(err, ErrHealthcheck):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: err.Error()})
	default:
		h.logger.Error("databases: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func newDatabaseResponse(database Database) databaseResponse {
	return databaseResponse{
		ID:              database.ID.String(),
		Name:            database.Name,
		EnvironmentID:   database.EnvironmentID.String(),
		EnvironmentName: database.EnvironmentName,
		ProjectID:       database.ProjectID.String(),
		ProjectName:     database.ProjectName,
		Engine:          database.Engine,
		Version:         database.Version,
		Status:          database.Status,
		StatusMessage:   database.StatusMessage,
		ServerID:        database.ServerID.String(),
		ServerName:      database.ServerName,
		ContainerID:     database.ContainerID,
		PublicPort:      database.PublicPort,
		Volume:          database.StoragePath,
		RuntimeHealth:   database.RuntimeHealth,
		CreatedAt:       database.CreatedAt,
		UpdatedAt:       database.UpdatedAt,
	}
}

func databaseFilter(w http.ResponseWriter, r *http.Request) (DatabaseFilter, bool) {
	var filter DatabaseFilter
	if raw := strings.TrimSpace(r.URL.Query().Get("environment_id")); raw != "" {
		envID, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid environment id"})
			return DatabaseFilter{}, false
		}
		filter.EnvironmentID = envID
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		projectID, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid project id"})
			return DatabaseFilter{}, false
		}
		filter.ProjectID = projectID
	}
	return filter, true
}

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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
