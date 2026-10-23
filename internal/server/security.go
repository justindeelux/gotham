package server

import (
	"net/http"
	"strings"

	"github.com/justindeelux/gotham/internal/auth"
)

var contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: " + strings.Join(auth.AvatarImgSources(), " ") + "; base-uri 'self'; object-src 'none'; form-action 'self' https://github.com; frame-ancestors 'none'"

const hstsHeader = "max-age=31536000"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		if s.isSecureRequest(r) {
			header.Set("Strict-Transport-Security", hstsHeader)
		}
		if isCredentialPath(r.URL.Path) {
			header.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func isCredentialPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/auth/") || strings.HasPrefix(path, "/api/v1/tokens")
}
