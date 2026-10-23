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

const TeamHeader = "X-Team-Id"

func (s *Server) RequireTeam(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, strict := r.Header[http.CanonicalHeaderKey(mcpContractHeader)]; strict {
			s.requireMCPTeam(next).ServeHTTP(w, r)
			return
		}
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

func (s *Server) withTeam() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.RequireAuth(requireResourceScopes(s.RequireTeam(s.teamWriteGate(next))))
	}
}

func (s *Server) readScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(RequireScopes(auth.ScopeRead)(next))
}

func (s *Server) adminScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(RequireScopes(auth.ScopeAdmin)(next))
}

func (s *Server) resourceScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(requireResourceScopes(next))
}
