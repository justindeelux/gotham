package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

// ServerService is the subset of servers.ServerService the HTTP layer depends
// on. Keeping it an interface lets tests substitute a fake without a database.
type ServerService interface {
	Add(ctx context.Context, userID uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID) (*servers.Server, error)
	List(ctx context.Context) ([]servers.Server, error)
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Validate(ctx context.Context, id uuid.UUID, auth servers.ValidateAuth) (*servers.ValidationResult, error)
	AddPrivateKey(ctx context.Context, name, privateKeyPEM string) (*servers.PrivateKey, error)
}

// createServerRequest is the body of POST /api/v1/servers.
type createServerRequest struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	SSHUser  string `json:"ssh_user"`
	SSHKeyID string `json:"ssh_key_id"`
}

// validateServerRequest is the optional body of POST /api/v1/servers/{id}/validate.
type validateServerRequest struct {
	SSHKeyID string `json:"ssh_key_id"`
	Password string `json:"password"`
}

// createPrivateKeyRequest is the body of POST /api/v1/private-keys.
type createPrivateKeyRequest struct {
	Name       string `json:"name"`
	PrivateKey string `json:"private_key"`
}

// serverDTO is the wire representation of a managed server.
type serverDTO struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	IP             string     `json:"ip"`
	Port           int        `json:"port"`
	SSHUser        string     `json:"ssh_user"`
	SSHKeyID       *string    `json:"ssh_key_id,omitempty"`
	Status         string     `json:"status"`
	NodeID         *string    `json:"node_id,omitempty"`
	OS             *string    `json:"os,omitempty"`
	DockerVersion  *string    `json:"docker_version,omitempty"`
	Arch           *string    `json:"arch,omitempty"`
	TotalMem       *int64     `json:"total_mem,omitempty"`
	TotalDisk      *int64     `json:"total_disk,omitempty"`
	CPUUsage       *float64   `json:"cpu_usage,omitempty"`
	MemUsage       *float64   `json:"mem_usage,omitempty"`
	DiskUsage      *float64   `json:"disk_usage,omitempty"`
	ContainerCount *int64     `json:"container_count,omitempty"`
	LastSeen       *time.Time `json:"last_seen,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// serverEnvelope wraps a single server.
type serverEnvelope struct {
	Server serverDTO `json:"server"`
}

// serverListEnvelope wraps a list of servers.
type serverListEnvelope struct {
	Servers []serverDTO `json:"servers"`
}

// validateResponse reports the per-check validation outcome.
type validateResponse struct {
	Message string                `json:"message,omitempty"`
	Checks  []servers.CheckResult `json:"checks"`
	Server  *serverDTO            `json:"server,omitempty"`
}

// privateKeyResponse is the body returned when a private key is created. The
// key material is never returned.
type privateKeyResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// mountServerRoutes registers the authenticated node-management endpoints under
// /api.
func (s *Server) mountServerRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth)
		protected.Get("/v1/servers", s.handleListServers)
		protected.Post("/v1/servers", s.handleCreateServer)
		protected.Get("/v1/servers/{id}", s.handleGetServer)
		protected.Delete("/v1/servers/{id}", s.handleDeleteServer)
		protected.Post("/v1/servers/{id}/validate", s.handleValidateServer)
		protected.Post("/v1/private-keys", s.handleCreatePrivateKey)
	})
}

// handleListServers returns every managed server.
func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	items, err := s.servers.List(r.Context())
	if err != nil {
		s.writeServerError(w, "list", err)
		return
	}

	response := serverListEnvelope{Servers: make([]serverDTO, 0, len(items))}
	for i := range items {
		response.Servers = append(response.Servers, newServerDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, response)
}

// handleCreateServer registers a new server.
func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req createServerRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	keyID, ok := parseOptionalUUID(w, req.SSHKeyID, "ssh_key_id")
	if !ok {
		return
	}

	created, err := s.servers.Add(r.Context(), userID, req.Name, req.IP, req.Port, req.SSHUser, keyID)
	if err != nil {
		s.writeServerError(w, "create", err)
		return
	}

	writeJSON(w, http.StatusCreated, serverEnvelope{Server: newServerDTO(created)})
}

// handleGetServer returns one server.
func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	found, err := s.servers.Get(r.Context(), id)
	if err != nil {
		s.writeServerError(w, "get", err)
		return
	}
	writeJSON(w, http.StatusOK, serverEnvelope{Server: newServerDTO(found)})
}

// handleDeleteServer removes one server.
func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	if err := s.servers.Delete(r.Context(), id); err != nil {
		s.writeServerError(w, "delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleValidateServer runs the SSH probes and reports each check.
func (s *Server) handleValidateServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	var req validateServerRequest
	if !s.decodeOptionalJSON(w, r, &req) {
		return
	}

	keyID, ok := parseOptionalUUID(w, req.SSHKeyID, "ssh_key_id")
	if !ok {
		return
	}

	result, err := s.servers.Validate(r.Context(), id, servers.ValidateAuth{KeyID: keyID, Password: req.Password})
	if err != nil {
		// A failed check is a 422 with the structured check list; a missing
		// server or bad request maps to 404/400.
		if result != nil && errors.Is(err, servers.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, validateResponse{
				Message: err.Error(),
				Checks:  result.Checks,
				Server:  serverDTOPtr(result.Server),
			})
			return
		}
		s.writeServerError(w, "validate", err)
		return
	}

	writeJSON(w, http.StatusOK, validateResponse{
		Checks: result.Checks,
		Server: serverDTOPtr(result.Server),
	})
}

// handleCreatePrivateKey stores an encrypted SSH private key.
func (s *Server) handleCreatePrivateKey(w http.ResponseWriter, r *http.Request) {
	var req createPrivateKeyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	created, err := s.servers.AddPrivateKey(r.Context(), req.Name, req.PrivateKey)
	if err != nil {
		s.writeServerError(w, "create private key", err)
		return
	}

	s.logger.Debug("servers: private key created", "private_key_id", created.ID.String())
	writeJSON(w, http.StatusCreated, privateKeyResponse{
		ID:        created.ID.String(),
		Name:      created.Name,
		CreatedAt: created.CreatedAt,
	})
}

// writeServerError maps a servers domain error to its HTTP response.
func (s *Server) writeServerError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, servers.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, servers.ErrValidation), errors.Is(err, servers.ErrNoCredentials):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, servers.ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
	default:
		s.logger.Error("servers: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// serverIDParam parses the {id} path parameter, answering 400 on a bad UUID.
func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

// parseOptionalUUID parses an optional UUID string, answering 400 when it is
// neither empty nor a valid UUID.
func parseOptionalUUID(w http.ResponseWriter, raw, field string) (uuid.UUID, bool) {
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid " + field})
		return uuid.Nil, false
	}
	return id, true
}

// decodeOptionalJSON decodes an optional body, treating an empty body as an
// empty struct.
func (s *Server) decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		return true
	}
	err := decodeJSONBody(w, r, dst)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
	return false
}

// newServerDTO maps a domain server to its wire representation.
func newServerDTO(server *servers.Server) serverDTO {
	dto := serverDTO{
		ID:             server.ID.String(),
		Name:           server.Name,
		IP:             server.IP,
		Port:           server.Port,
		SSHUser:        server.SSHUser,
		Status:         server.Status,
		NodeID:         server.NodeID,
		OS:             server.OS,
		DockerVersion:  server.DockerVersion,
		Arch:           server.Arch,
		TotalMem:       server.TotalMem,
		TotalDisk:      server.TotalDisk,
		CPUUsage:       server.CPUUsage,
		MemUsage:       server.MemUsage,
		DiskUsage:      server.DiskUsage,
		ContainerCount: server.ContainerCount,
		LastSeen:       server.LastSeen,
		CreatedAt:      server.CreatedAt,
		UpdatedAt:      server.UpdatedAt,
	}
	if server.SSHKeyID != nil {
		keyID := server.SSHKeyID.String()
		dto.SSHKeyID = &keyID
	}
	return dto
}

// serverDTOPtr maps an optional domain server.
func serverDTOPtr(server *servers.Server) *serverDTO {
	if server == nil {
		return nil
	}
	dto := newServerDTO(server)
	return &dto
}
