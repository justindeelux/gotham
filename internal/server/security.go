package server

import "net/http"

// contentSecurityPolicy is a conservative, static-safe policy that still allows
// the embedded Vite SPA: same-origin scripts and styles plus the inline styles
// Naive UI injects at runtime.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:"

// securityHeaders sets the baseline security response headers on every
// response, including errors and the SPA shell.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}
