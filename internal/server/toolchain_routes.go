package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/toolchain"
)

// The toolchain routes live under the servers API and ride the same team chain
// as server management: reads need the read scope, enqueue/cancel need the
// deploy scope and an owner/admin role, and the domain re-checks the node's
// team (so a foreign node answers not-found). They mount whenever a database is
// wired; the agent dialer is optional, so a build without it still serves job
// reads while agent calls answer 502.

// toolDialer is the mTLS ToolService dial implemented by *servers.ServerService.
// The HTTP layer type-asserts its server registry to this interface, so a server
// built with a fake registry simply leaves the dialer unwired: the job routes
// still mount, but a catalog or enqueue call answers agent-unavailable.
type toolDialer interface {
	DialToolClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.ToolClient, error)
}

// toolchainService builds the toolchain domain service for the HTTP wiring from
// the shared database and the (optional) agent dialer. It returns nil, so the
// routes are not mounted, when there is no database. The node registry is passed
// for the per-node team authorization; the instance id defaults to the host name
// (see GOTHAM_INSTANCE_ID) for lease-based job recovery.
func (s *Server) toolchainService() *toolchain.Service {
	if s.persistence == nil {
		return nil
	}
	cfg := toolchain.Config{
		Store:     s.persistence,
		Registry:  s.servers,
		RedisAddr: s.cfg.Snapshot().Redis.Addr,
		Logger:    s.logger,
	}
	if dialer, ok := s.servers.(toolDialer); ok {
		cfg.Dial = func(ctx context.Context, serverID uuid.UUID) (toolchain.AgentConn, error) {
			return dialer.DialToolClient(ctx, serverID)
		}
	}
	return toolchain.NewDefaultService(cfg)
}

// toolchainToolsEnvelope is the GET .../tools body.
type toolchainToolsEnvelope struct {
	Tools []toolchain.Tool `json:"tools"`
}

// toolchainJobsEnvelope is the GET .../toolchain/jobs body.
type toolchainJobsEnvelope struct {
	Jobs []toolchain.Job `json:"jobs"`
}

// toolchainJobEnvelope wraps a single job (cancel).
type toolchainJobEnvelope struct {
	Job toolchain.Job `json:"job"`
}

// toolchainJobDetailEnvelope is the GET .../jobs/{jobID} body: the job plus a
// page of its log. NextSeq is the highest seq returned, so a client polls
// ?after=NextSeq for the rest instead of pulling the whole log every time.
type toolchainJobDetailEnvelope struct {
	Job     toolchain.Job `json:"job"`
	Log     string        `json:"log"`
	NextSeq int64         `json:"next_seq"`
}

// enqueueJobsRequest is the POST .../toolchain/jobs body: a multi-select of
// catalog tool ids and the action to apply to all of them.
type enqueueJobsRequest struct {
	Tools  []string `json:"tools"`
	Action string   `json:"action"`
}

// mountToolchainRoutes registers the toolchain job surface under /api. It is a
// no-op when no service is wired.
func (s *Server) mountToolchainRoutes(api chi.Router) {
	if s.toolchain == nil {
		return
	}
	api.Group(func(protected chi.Router) {
		protected.Use(s.withTeam())
		protected.Get("/v1/servers/{id}/tools", s.handleListTools)
		protected.Get("/v1/servers/{id}/toolchain/jobs", s.handleListToolchainJobs)
		protected.Post("/v1/servers/{id}/toolchain/jobs", s.handleEnqueueToolchainJobs)
		protected.Get("/v1/servers/{id}/toolchain/jobs/{jobID}", s.handleGetToolchainJob)
		protected.Post("/v1/servers/{id}/toolchain/jobs/{jobID}/cancel", s.handleCancelToolchainJob)
	})
}

// handleListTools returns a node's tool catalog with each entry's detected
// state (GET /api/v1/servers/{id}/tools).
func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	tools, err := s.toolchain.ListTools(r.Context(), serverID)
	if err != nil {
		s.writeToolchainError(w, "list tools", err)
		return
	}
	if tools == nil {
		tools = []toolchain.Tool{}
	}
	writeJSON(w, http.StatusOK, toolchainToolsEnvelope{Tools: tools})
}

// handleListToolchainJobs returns a node's jobs, newest first
// (GET /api/v1/servers/{id}/toolchain/jobs).
func (s *Server) handleListToolchainJobs(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := parsePositiveInt(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid limit"})
			return
		}
		limit = parsed
	}
	jobs, err := s.toolchain.List(r.Context(), serverID, limit)
	if err != nil {
		s.writeToolchainError(w, "list jobs", err)
		return
	}
	if jobs == nil {
		jobs = []toolchain.Job{}
	}
	writeJSON(w, http.StatusOK, toolchainJobsEnvelope{Jobs: jobs})
}

// handleEnqueueToolchainJobs enqueues one job per selected tool
// (POST /api/v1/servers/{id}/toolchain/jobs).
func (s *Server) handleEnqueueToolchainJobs(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req enqueueJobsRequest
	if !s.decodeOptionalJSON(w, r, &req) {
		return
	}
	action := toolchain.Action(req.Action)
	result, err := s.toolchain.Enqueue(r.Context(), serverID, action, req.Tools)
	if err != nil {
		s.writeToolchainError(w, "enqueue jobs", err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// handleGetToolchainJob returns one job plus a page of its log
// (GET /api/v1/servers/{id}/toolchain/jobs/{jobID}). ?after=N returns only log
// lines with seq > N, so a client tails a running job incrementally.
func (s *Server) handleGetToolchainJob(w http.ResponseWriter, r *http.Request) {
	serverID, jobID, ok := toolchainJobParams(w, r)
	if !ok {
		return
	}
	after, ok := parseAfterQuery(w, r)
	if !ok {
		return
	}
	detail, err := s.toolchain.Get(r.Context(), serverID, jobID, after)
	if err != nil {
		s.writeToolchainError(w, "get job", err)
		return
	}
	writeJSON(w, http.StatusOK, toolchainJobDetailEnvelope{Job: detail.Job, Log: detail.Log, NextSeq: detail.NextSeq})
}

// handleCancelToolchainJob cancels one queued job
// (POST /api/v1/servers/{id}/toolchain/jobs/{jobID}/cancel).
func (s *Server) handleCancelToolchainJob(w http.ResponseWriter, r *http.Request) {
	serverID, jobID, ok := toolchainJobParams(w, r)
	if !ok {
		return
	}
	job, err := s.toolchain.Cancel(r.Context(), serverID, jobID)
	if err != nil {
		s.writeToolchainError(w, "cancel job", err)
		return
	}
	writeJSON(w, http.StatusOK, toolchainJobEnvelope{Job: job})
}

// toolchainJobParams parses the {id} and {jobID} path parameters, answering 400
// when either is malformed.
func toolchainJobParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	jobID, err := uuid.Parse(chi.URLParam(r, "jobID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid job id"})
		return uuid.Nil, uuid.Nil, false
	}
	return serverID, jobID, true
}

// writeToolchainError maps the domain's sentinel errors onto status codes. An
// unknown error is a real server failure (a DB problem), logged and answered
// 500, not disguised as a retryable agent failure.
func (s *Server) writeToolchainError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, toolchain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, toolchain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, apiError{Message: "insufficient team role"})
	case errors.Is(err, toolchain.ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, toolchain.ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
	case errors.Is(err, toolchain.ErrAgentUnavailable):
		writeJSON(w, http.StatusBadGateway, apiError{Message: "agent unavailable"})
	default:
		s.logger.Error("toolchain: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// parsePositiveInt parses a strictly positive base-10 integer.
func parsePositiveInt(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errors.New("not a positive integer")
	}
	return n, nil
}

// parseAfterQuery reads the optional ?after= log cursor (a non-negative seq);
// absent means 0.
func parseAfterQuery(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid after"})
		return 0, false
	}
	return n, true
}
