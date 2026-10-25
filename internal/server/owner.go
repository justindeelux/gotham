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
// immediately. Operator API tokens additionally need the admin scope.
func (s *Server) isOwner(r *http.Request) bool {
	if scopes, isAPIToken := ScopesFromContext(r.Context()); isAPIToken {
		if !auth.ScopesContain(scopes, auth.ScopeAdmin) {
			return false
		}
	}
	return s.isOwnerAccount(r)
}

// isOwnerAccount checks only the account role. Resource routes pair it with
// their normal read/deploy scope gate, so an owner's read or deploy API token
// keeps working there; only platform-operator routes require the admin scope.
func (s *Server) isOwnerAccount(r *http.Request) bool {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || s.auth == nil {
		return false
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

func (s *Server) requireOwnerAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwnerAccount(r) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "owner access is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

const ownerScopeDenied = "requesting the admin scope requires owner access"

// resourceAuth gates instance-level resource routes. Only the owner reaches
// them until per-project grants land (GOTHAM-65).
func (s *Server) resourceAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.RequireAuth(requireResourceScopes(s.requireOwnerAccount(next)))
	}
}
