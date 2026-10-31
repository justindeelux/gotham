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

type toolDialer interface {
	DialToolClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.ToolClient, error)
}

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

type toolchainToolsEnvelope struct {
	Tools []toolchain.Tool `json:"tools"`
}

type toolchainJobsEnvelope struct {
	Jobs []toolchain.Job `json:"jobs"`
}

type toolchainJobEnvelope struct {
	Job toolchain.Job `json:"job"`
}

type toolchainJobDetailEnvelope struct {
	Job     toolchain.Job `json:"job"`
	Log     string        `json:"log"`
	NextSeq int64         `json:"next_seq"`
}

type enqueueJobsRequest struct {
	Tools  []string `json:"tools"`
	Action string   `json:"action"`
}

func (s *Server) mountToolchainRoutes(api chi.Router) {
	if s.toolchain == nil {
		return
	}
	api.Group(func(readOnly chi.Router) {
		readOnly.Use(s.resourceAuth())
		readOnly.Get("/v1/servers/{id}/tools", s.handleListTools)
		readOnly.Get("/v1/servers/{id}/toolchain/jobs", s.handleListToolchainJobs)
		readOnly.Get("/v1/servers/{id}/toolchain/jobs/{jobID}", s.handleGetToolchainJob)
	})
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth, s.RequireOwner)
		protected.Post("/v1/servers/{id}/toolchain/jobs", s.handleEnqueueToolchainJobs)
		protected.Post("/v1/servers/{id}/toolchain/jobs/{jobID}/cancel", s.handleCancelToolchainJob)
	})
}

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

func (s *Server) writeToolchainError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, toolchain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, toolchain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, apiError{Message: "forbidden"})
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

func parsePositiveInt(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errors.New("not a positive integer")
	}
	return n, nil
}

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
