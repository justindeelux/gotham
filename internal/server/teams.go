package server

import (
	"context"
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

// authorizeLogSubscription authorizes one WebSocket container-log subscription:
// the node must exist and, when it belongs to a team, the caller must be a
// member of that team (every role may read logs). A legacy node without a team
// stays shared, matching the rest of the node surface. A nil team service (no
// database) leaves the subscription open, like the other pre-teams
// compatibility paths.
func (s *Server) authorizeLogSubscription(ctx context.Context, serverID, userID uuid.UUID) error {
	if s.servers == nil {
		return errors.New("ws: server registry is not configured")
	}
	server, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	if server.TeamID == uuid.Nil || s.teamService == nil {
		return nil
	}
	if _, err := s.teamService.Membership(ctx, server.TeamID, userID); err != nil {
		return err
	}
	return nil
}

// authorizeTaskSubscription authorizes one WebSocket background-task
// subscription: the caller must be a member of the channel's team, so one
// team's deploy progress never leaks to another. A nil team service (no
// database) leaves the subscription open, like the other pre-teams
// compatibility paths.
func (s *Server) authorizeTaskSubscription(ctx context.Context, teamID string, userID uuid.UUID) error {
	if s.teamService == nil {
		return nil
	}
	teamUUID, err := uuid.Parse(teamID)
	if err != nil {
		return err
	}
	if _, err := s.teamService.Membership(ctx, teamUUID, userID); err != nil {
		return err
	}
	return nil
}

// withTeam builds the middleware chain of a team-scoped resource group:
// authentication, the API-token scope boundary (reads need read, mutations
// need deploy), active-team resolution, and the owner/admin gate for mutating
// methods. The control plane passes the same chain to every resource package's
// Mount, so a new route in one of those groups cannot silently miss either
// team scoping or the scope boundary.
func (s *Server) withTeam() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.RequireAuth(requireResourceScopes(s.RequireTeam(s.teamWriteGate(next))))
	}
}

// readScopeAuth wraps a handler with authentication and the read scope, for
// read-only surfaces reached by API tokens.
func (s *Server) readScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(RequireScopes(auth.ScopeRead)(next))
}

// adminScopeAuth wraps a handler with authentication and the admin scope, for
// platform-management surfaces reached by API tokens. A JWT session holds every
// scope, so it passes.
func (s *Server) adminScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(RequireScopes(auth.ScopeAdmin)(next))
}

// resourceScopeAuth wraps a handler with authentication and the method-based
// resource scope boundary (read for GET/HEAD, deploy otherwise), for resource
// groups that do not ride the team chain.
func (s *Server) resourceScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(requireResourceScopes(next))
}
