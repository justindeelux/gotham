package server

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/teams"
)

// TeamHeader selects the active team of a request. Absent (or when the teams
// feature is off) the caller's personal team is used, so every pre-teams client
// keeps seeing exactly what it saw before teams existed.
const TeamHeader = "X-Team-Id"

// RequireTeam resolves the request's active team after RequireAuth and stores
// it in the request context (teams.FromContext). The header must be a UUID and
// the caller must be a member of the named team; a non-member is answered 403.
// Without the header the caller's personal team (whose ID is the user's ID) is
// used, which needs no database read.
//
// A nil team service (no database: the handler tests) leaves the request
// without a team scope, and every resource package keeps its pre-teams,
// creator-scoped behavior.
func (s *Server) RequireTeam(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.teamService == nil {
			next.ServeHTTP(w, r)
			return
		}

		userID, ok := UserIDFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}

		scope := teams.Scope{UserID: userID}
		raw := strings.TrimSpace(r.Header.Get(TeamHeader))
		if raw == "" || !teams.Enabled() {
			// The personal team: ID is the user's ID, owner by construction.
			scope.TeamID = teams.PersonalTeamID(userID)
			scope.Role = teams.RoleOwner
		} else {
			teamID, err := uuid.Parse(raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid X-Team-Id header"})
				return
			}
			role, err := s.teamService.Membership(r.Context(), teamID, userID)
			if err != nil {
				if errors.Is(err, teams.ErrNotFound) {
					writeJSON(w, http.StatusForbidden, apiError{Message: "not a member of this team"})
					return
				}
				s.logger.Error("teams: resolve active team", "error", err)
				writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
				return
			}
			scope.TeamID, scope.Role = teamID, role
		}

		next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
	})
}

// RequireTeamRole gates a handler on the caller's role in the active team. A
// request without a team scope (teams not wired) passes, matching the pre-teams
// behavior. It is a route-level pre-filter: the teams service re-checks the
// role of the team each call names.
func RequireTeamRole(allowed ...teams.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := teams.FromContext(r.Context())
			if !ok || !scope.Active() || slices.Contains(allowed, scope.Role) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, apiError{Message: "insufficient team role"})
		})
	}
}

// teamWriteGate admits read methods for every team member and requires
// owner/admin for everything else, so a read_only member can read but every
// mutation is refused with 403 before it reaches a domain service. Domains
// re-check the resource's team and the caller's role, so this is a fast
// pre-filter, not the only check.
func (s *Server) teamWriteGate(next http.Handler) http.Handler {
	write := RequireTeamRole(teams.RoleOwner, teams.RoleAdmin)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		write.ServeHTTP(w, r)
	})
}

// withTeam builds the middleware chain of a team-scoped resource group:
// authentication, the admin scope when the group requires it, active-team
// resolution, and the owner/admin gate for mutating methods. The control plane
// passes the same chain to every resource package's Mount, so a new route in
// one of those groups cannot silently miss team scoping.
func (s *Server) withTeam(adminScope bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := s.RequireTeam(s.teamWriteGate(next))
		if adminScope {
			h = RequireScopes(auth.ScopeAdmin)(h)
		}
		return s.RequireAuth(h)
	}
}
