package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// syncRequest is the optional body of POST /v1/proxy/sync: with a server_id
// only that node is synced, otherwise every node hosting a proxied
// application is.
type syncRequest struct {
	ServerID string `json:"server_id"`
}

// syncResponse reports the per-node outcome of a sync run.
type syncResponse struct {
	Results []syncResult `json:"results"`
}

// syncResult is one node's outcome on the wire.
type syncResult struct {
	ServerID string `json:"server_id"`
	Synced   bool   `json:"synced"`
	Error    string `json:"error,omitempty"`
}

// Mount registers the authenticated proxy endpoints under /api. A nil service
// or FEATURE_PROXY=false mounts nothing.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc ProxyService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/proxy/sync", h.sync)
	})
}

// handler serves the proxy routes.
type handler struct {
	svc    ProxyService
	logger *slog.Logger
}

// sync regenerates the routing configuration and pushes it to the node
// agents. A partial failure answers 502 with the per-node results, so the
// operator sees which node rejected the reload.
func (h *handler) sync(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	serverID, ok := parseOptionalServerID(w, req.ServerID)
	if !ok {
		return
	}

	if serverID != uuid.Nil {
		if err := h.svc.SyncServer(r.Context(), serverID); err != nil {
			h.writeError(w, "sync", serverID, err)
			return
		}
		writeJSON(w, http.StatusOK, syncResponse{Results: []syncResult{{ServerID: serverID.String(), Synced: true}}})
		return
	}

	results, err := h.svc.SyncAll(r.Context())
	if err != nil {
		h.writeServerError(w, "sync", err)
		return
	}
	response := syncResponse{Results: make([]syncResult, 0, len(results))}
	failed := false
	for _, result := range results {
		entry := syncResult{ServerID: result.ServerID.String(), Synced: result.Error == ""}
		if result.Error != "" {
			entry.Error = result.Error
			failed = true
		}
		response.Results = append(response.Results, entry)
	}
	status := http.StatusOK
	if failed {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, response)
}

// writeError maps one node's sync failure to its HTTP response.
func (h *handler) writeError(w http.ResponseWriter, op string, serverID uuid.UUID, err error) {
	response := syncResponse{Results: []syncResult{{ServerID: serverID.String(), Error: err.Error()}}}
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, response)
	case errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, response)
	case errors.Is(err, ErrAgentUnavailable), errors.Is(err, ErrReload):
		writeJSON(w, http.StatusBadGateway, response)
	default:
		h.logger.Error("proxy: "+op, "server_id", serverID.String(), "error", err)
		writeJSON(w, http.StatusInternalServerError, response)
	}
}

// writeServerError maps a whole-run failure to its HTTP response.
func (h *handler) writeServerError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	default:
		h.logger.Error("proxy: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// apiError is the JSON body returned for whole-run failures.
type apiError struct {
	Message string `json:"message"`
}

// decodeOptionalBody decodes an optional JSON body, treating an empty body as
// an empty struct, and answers 400 on malformed JSON.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		return true
	}
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
	return false
}

// parseOptionalServerID parses the optional server_id, answering 400 when it
// is neither empty nor a valid UUID.
func parseOptionalServerID(w http.ResponseWriter, raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server_id"})
		return uuid.Nil, false
	}
	return id, true
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
