package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

// errAccessDenied is a refusal a caller may retry only after its role changes.
var errAccessDenied = errors.New("access denied")

// authorizeLogSubscription admits only the owner to container logs until
// per-project grants exist (GOTHAM-65).
func (s *Server) authorizeLogSubscription(ctx context.Context, serverID, userID uuid.UUID) error {
	if s.servers == nil {
		return errors.New("ws: server registry is not configured")
	}
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return err
	}
	return s.requireOwnerContext(ctx, userID)
}

// authorizeTaskSubscription admits only the owner to a project's task feed
// until per-project grants exist (GOTHAM-65).
func (s *Server) authorizeTaskSubscription(ctx context.Context, projectID string, userID uuid.UUID) error {
	if _, err := uuid.Parse(projectID); err != nil {
		return err
	}
	return s.requireOwnerContext(ctx, userID)
}

func (s *Server) requireOwnerContext(ctx context.Context, userID uuid.UUID) error {
	if s.auth == nil {
		return errAccessDenied
	}
	user, err := s.auth.Me(ctx, userID)
	if err != nil {
		return err
	}
	if user.Role != auth.RoleOwner {
		return errAccessDenied
	}
	return nil
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
