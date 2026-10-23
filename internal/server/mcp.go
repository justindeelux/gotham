package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/teams"
)

const mcpContractHeader = "X-Gotham-MCP-Contract"
const mcpTeamHeader = "X-Gotham-MCP-Team-Id"

func (s *Server) requireMCPTeam(next http.Handler) http.Handler {
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
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == uuid.Nil {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}
		teamID, err := uuid.Parse(r.Header.Get(TeamHeader))
		if err != nil || teamID == uuid.Nil {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "explicit nonzero team UUID required"})
			return
		}
		if s.teamService == nil || !teams.Enabled() {
			writeJSON(w, http.StatusServiceUnavailable, apiError{Message: "strict teams unavailable"})
			return
		}
		role, err := s.teamService.Membership(r.Context(), teamID, userID)
		if err != nil {
			if errors.Is(err, teams.ErrNotFound) {
				writeJSON(w, http.StatusForbidden, apiError{Message: "not a member of this team"})
			} else {
				writeJSON(w, http.StatusInternalServerError, apiError{Message: "team resolution failed"})
			}
			return
		}
		w.Header().Set(mcpContractHeader, "1")
		w.Header().Set(mcpTeamHeader, teamID.String())
		scope := teams.Scope{UserID: userID, TeamID: teamID, Role: role}
		next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
	})
}

func (s *Server) mountMCPRoutes(api chi.Router) {
	api.With(s.readScopeAuth, s.requireMCPTeam).Get("/v1/mcp/context", func(w http.ResponseWriter, r *http.Request) {
		scope, _ := teams.FromContext(r.Context())
		capabilities := []string{"context", "strict_reads"}
		if s.persistence != nil && s.deploy != nil && deploy.Enabled() {
			capabilities = append(capabilities, "strict_mutations")
			if !s.ephemeralSecret {
				capabilities = append(capabilities, "request_replay")
			}
		}
		writeJSON(w, http.StatusOK, struct {
			TeamID       uuid.UUID `json:"team_id"`
			Capabilities []string  `json:"capabilities"`
		}{scope.TeamID, capabilities})
	})
}
