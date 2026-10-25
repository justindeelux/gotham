package server

import (
	"errors"
	"net/http"

	"github.com/justindeelux/gotham/internal/auth"
)

func (s *Server) RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwner(r) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "owner access is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isOwner resolves the caller's current role from the database on every
// operator request, so a demoted owner's sessions and API tokens lose access
// immediately. API tokens additionally need the admin scope.
func (s *Server) isOwner(r *http.Request) bool {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || s.auth == nil {
		return false
	}
	if scopes, isAPIToken := ScopesFromContext(r.Context()); isAPIToken {
		if !auth.ScopesContain(scopes, auth.ScopeAdmin) {
			return false
		}
	}
	user, err := s.auth.Me(r.Context(), userID)
	if err != nil {
		if !errors.Is(err, auth.ErrUnauthorized) && s.logger != nil {
			s.logger.Error("server: owner check", "error", err)
		}
		return false
	}
	return user.Role == auth.RoleOwner
}

const ownerScopeDenied = "requesting the admin scope requires owner access"
