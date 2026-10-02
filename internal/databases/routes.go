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

	"github.com/justindeelux/gotham/internal/teams"
)

// maxBodyBytes bounds database request bodies (they are tiny).
const maxBodyBytes = 1 << 20 // 1 MiB

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors deploy.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// databaseResponse is the wire representation of a database. Sealed secrets
// never appear here — credentials are served by the credentials endpoint to
// their owner only.
type databaseResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Engine      string    `json:"engine"`
	Version     string    `json:"version,omitempty"`
	Status      Status    `json:"status"`
	ServerID    string    `json:"server_id"`
	ContainerID string    `json:"container_id,omitempty"`
	PublicPort  int32     `json:"public_port"`
	Volume      string    `json:"volume"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// databaseEnvelope wraps a single database.
type databaseEnvelope struct {
	Database databaseResponse `json:"database"`
}

// databaseListEnvelope wraps a database list.
type databaseListEnvelope struct {
	Databases []databaseResponse `json:"databases"`
}

// createDatabaseEnvelope is the POST /v1/databases response: the row plus the
// credentials it was created with, so the operator can connect immediately.
type createDatabaseEnvelope struct {
	Database    databaseResponse `json:"database"`
	Credentials Credentials      `json:"credentials"`
}

// credentialsEnvelope wraps decrypted credentials for their owner.
type credentialsEnvelope struct {
	Credentials Credentials `json:"credentials"`
}

// createRequest is the body of POST /v1/databases.
type createRequest struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
	// Version is the optional image tag; empty selects the engine default.
	Version string `json:"version,omitempty"`
	// ServerID is the node that runs the database.
	ServerID string `json:"server_id"`
	// PublicPort optionally publishes the engine port on the host; 0 keeps the
	// database internal to the node. It is fixed at creation because Docker
	// port bindings cannot change on a live container.
	PublicPort int32 `json:"public_port,omitempty"`
}

// updateRequest is the body of PATCH /v1/databases/{id}. It aliases the
// service-level UpdateRequest so the handler decodes straight into the value
// it will send.
type updateRequest = UpdateRequest

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the database routes for one DatabaseService.
type handler struct {
	svc    DatabaseService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated database endpoints on r:
//
//	POST   /v1/databases
//	GET    /v1/databases
//	GET    /v1/databases/{id}
//	PATCH  /v1/databases/{id}
//	DELETE /v1/databases/{id}
//	GET    /v1/databases/{id}/credentials
//	POST   /v1/databases/{id}/start
//	POST   /v1/databases/{id}/stop
//	POST   /v1/databases/{id}/restart
//
// auth wraps the group (the server passes its team chain); deployScope is the
// server's deploy-scope middleware, applied explicitly to the credentials read
// because it returns decrypted passwords. The resource classifier already
// deploy-gates secret-bearing reads; this wrap is defense in depth, so a future
// change to the classifier cannot silently reopen the endpoint. A nil svc or
// FEATURE_DATABASES=false mounts nothing, so the control plane can call Mount
// unconditionally.
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

// create serves POST .../databases: 201 with the row and its credentials.
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if !decodeBody(w, r, &req) {
		return
	}
	serverID, err := uuid.Parse(strings.TrimSpace(req.ServerID))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return
	}

	database, credentials, err := h.svc.Create(r.Context(), userID, CreateRequest{
		Name:       req.Name,
		Engine:     req.Engine,
		Version:    req.Version,
		ServerID:   serverID,
		PublicPort: req.PublicPort,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createDatabaseEnvelope{
		Database:    newDatabaseResponse(database),
		Credentials: credentials,
	})
}

// list serves GET .../databases.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	databases, err := h.svc.List(r.Context(), userID)
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

// get serves GET .../databases/{id}.
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

// credentials serves GET .../databases/{id}: the decrypted credentials of a
// database the caller owns.
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

// update serves PATCH .../databases/{id} (rename).
func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req updateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	database, err := h.svc.Update(r.Context(), userID, databaseID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, databaseEnvelope{Database: newDatabaseResponse(database)})
}

// delete serves DELETE .../databases/{id}: the container goes, the row is
// soft-deleted and the volume stays. 204 on success.
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

// start serves POST .../databases/{id}/start.
func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "start")
}

// stop serves POST .../databases/{id}/stop.
func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "stop")
}

// restart serves POST .../databases/{id}/restart.
func (h *handler) restart(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "restart")
}

// action dispatches one lifecycle verb and answers with the updated row.
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

// databaseParams resolves the authenticated user and the {id} path parameter,
// answering 401/400 as needed.
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
		writeJSON(w, http.StatusConflict, errorBody{Message: "a database with that name already exists"})
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

// newDatabaseResponse maps a domain database to its wire representation.
func newDatabaseResponse(database Database) databaseResponse {
	return databaseResponse{
		ID:          database.ID.String(),
		Name:        database.Name,
		Engine:      database.Engine,
		Version:     database.Version,
		Status:      database.Status,
		ServerID:    database.ServerID.String(),
		ContainerID: database.ContainerID,
		PublicPort:  database.PublicPort,
		Volume:      database.StoragePath,
		CreatedAt:   database.CreatedAt,
		UpdatedAt:   database.UpdatedAt,
	}
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
