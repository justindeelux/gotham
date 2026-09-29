package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/justindeelux/gotham/internal/auth"
)

// PlatformAdminsEnv lists the account emails allowed to use the
// platform-global operations (proxy sync, DNS-provider management) with a
// session (JWT). It is a comma-separated list, case-insensitive. Unset means
// no JWT email is a platform operator: only an API token holding the admin
// scope or a session whose role claim is "admin" passes. Operators must list
// themselves here (or mint an admin-scoped API token) to manage global DNS
// providers; per-application certificate and redirect management is unaffected
// and stays with the owning team.
const PlatformAdminsEnv = "PLATFORM_ADMINS"

// RequirePlatformAdmin gates the platform-global proxy surface. It passes:
//
//   - an API token holding the admin scope (the machine-operator path), or
//   - a session (JWT) whose role claim is "admin", or
//   - a session whose account email is listed in PLATFORM_ADMINS.
//
// Everything else is refused with 403, including every plain session. That is
// deliberately secure by default: the control plane has no admin-role users
// until an operator creates one, and a normal session must not be able to
// re-synchronize every node or read DNS credentials.
func (s *Server) RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isPlatformOperator(r) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "platform operator access is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isPlatformOperator reports whether the request's caller is a platform
// operator: an API token holding the admin scope, a session whose role claim is
// "admin", or a session whose account email is listed in PLATFORM_ADMINS.
// RequirePlatformAdmin and the admin-scope issuance gate in handleCreateToken
// share it, so the operator boundary can never be widened by one path and
// narrowed by the other.
func (s *Server) isPlatformOperator(r *http.Request) bool {
	if scopes, isAPIToken := ScopesFromContext(r.Context()); isAPIToken {
		return auth.ScopesContain(scopes, auth.ScopeAdmin)
	}

	if role, ok := RoleFromContext(r.Context()); ok && strings.EqualFold(role, "admin") {
		return true
	}

	if userID, ok := UserIDFromContext(r.Context()); ok && s.auth != nil {
		if user, err := s.auth.Me(r.Context(), userID); err == nil && s.isPlatformAdminEmail(user.Email) {
			return true
		}
	}
	return false
}

// platformAdminScopeDenied is the 403 body returned when a caller tries to
// mint the platform admin scope without being a platform operator.
const platformAdminScopeDenied = "requesting the admin scope requires platform operator access"

// isPlatformAdminEmail reports whether email is listed in PLATFORM_ADMINS.
func (s *Server) isPlatformAdminEmail(email string) bool {
	for _, allowed := range platformAdminEmails() {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(email)) {
			return true
		}
	}
	return false
}

// platformAdminEmails splits the PLATFORM_ADMINS list.
func platformAdminEmails() []string {
	raw := strings.TrimSpace(os.Getenv(PlatformAdminsEnv))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
