package server

import (
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

func (s *Server) isOwner(r *http.Request) bool {
	if scopes, isAPIToken := ScopesFromContext(r.Context()); isAPIToken {
		return auth.ScopesContain(scopes, auth.ScopeAdmin)
	}
	role, ok := RoleFromContext(r.Context())
	return ok && role == auth.RoleOwner
}

const ownerScopeDenied = "requesting the admin scope requires owner access"
