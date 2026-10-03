package server

import (
	"net/http"
	"strings"

	"github.com/justindeelux/gotham/internal/auth"
)

// contentSecurityPolicy is a conservative, static-safe policy that still allows
// the embedded Vite SPA: same-origin scripts and styles plus the inline styles
// Naive UI injects at runtime. img-src additionally allows the OAuth avatar
// hosts from auth.AvatarImgSources (GitHub avatar URLs would otherwise be
// blocked by the policy and the header would fall back to the initial); every
// other directive stays same-origin or stricter. The avatar hosts are defined
// once in the auth package and shared with the storage validator, so a stored
// avatar always renders under this policy.
// base-uri/object-src/form-action/frame-ancestors close the injected-base-tag,
// plugin-content, form-hijack and framing vectors; form-action does not fall
// back to default-src, so it is stated explicitly.
var contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: " + strings.Join(auth.AvatarImgSources(), " ") + "; base-uri 'self'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"

// hstsHeader pins browsers to HTTPS. It is emitted only on secure requests
// (direct TLS or a trusted proxy's X-Forwarded-Proto: https); a direct-HTTP
// deployment behind a proxy that does not forward the scheme must set HSTS at
// the proxy instead.
const hstsHeader = "max-age=31536000"

// securityHeaders sets the baseline security response headers on every
// response, including errors and the SPA shell.
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

// isCredentialPath reports whether path must never be cached: the auth surface
// (login, OAuth, refresh, logout) and the API-token routes carry credentials or
// freshly minted secrets in the request or response body.
func isCredentialPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/auth/") || strings.HasPrefix(path, "/api/v1/tokens")
}
