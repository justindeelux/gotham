package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/justindeelux/gotham/internal/auth"
)

const PlatformAdminsEnv = "PLATFORM_ADMINS"

func (s *Server) RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isPlatformOperator(r) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "platform operator access is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

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

const platformAdminScopeDenied = "requesting the admin scope requires platform operator access"

func (s *Server) isPlatformAdminEmail(email string) bool {
	for _, allowed := range platformAdminEmails() {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(email)) {
			return true
		}
	}
	return false
}

func platformAdminEmails() []string {
	raw := strings.TrimSpace(os.Getenv(PlatformAdminsEnv))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
