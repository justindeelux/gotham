package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/requestreplay"
)

const mcpContractHeader = "X-Gotham-MCP-Contract"

func (s *Server) requireMCPStrict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(mcpContractHeader) != "1" {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "unsupported MCP contract"})
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutation && (r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/v1/mcp/")) {
			writeJSON(w, http.StatusMethodNotAllowed, apiError{Message: "unsupported MCP operation"})
			return
		}
		if mutation {
			if !IsAPITokenRequest(r.Context()) {
				writeJSON(w, http.StatusForbidden, apiError{Message: "API token required"})
				return
			}
			keys := r.Header.Values("Idempotency-Key")
			if len(keys) != 1 || !requestreplay.ValidKey(keys[0]) {
				requestreplay.WriteError(w, requestreplay.ErrKey)
				return
			}
		}
		w.Header().Set(mcpContractHeader, "1")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) mountMCPRoutes(api chi.Router) {
	api.With(s.readScopeAuth, s.requireMCPStrict).Get("/v1/mcp/context", func(w http.ResponseWriter, r *http.Request) {
		capabilities := []string{"context", "strict_reads"}
		if s.persistence != nil && s.deploy != nil && deploy.Enabled() {
			capabilities = append(capabilities, "strict_mutations")
			if !s.ephemeralSecret {
				capabilities = append(capabilities, "request_replay")
			}
		}
		writeJSON(w, http.StatusOK, struct {
			Capabilities []string `json:"capabilities"`
		}{capabilities})
	})
}
