package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/auth"
)

// oauthStateCookieMaxAge is the state cookie lifetime in seconds. It matches the
// server-side state TTL.
const oauthStateCookieMaxAge = 600

// oauthFailureRedirect is where a failed callback sends the browser. The query
// never carries error details so nothing is leaked to logs or referrers.
const oauthFailureRedirect = "/login?error=oauth_failed"

// oauthCallbackPath is the SPA route that receives the token fragment after a
// successful callback.
const oauthCallbackPath = "/oauth/callback"

// OAuthService is the subset of auth.OAuthService the HTTP layer depends on.
// Keeping it an interface lets tests substitute a fake without a database or a
// real provider.
type OAuthService interface {
	Begin(ctx context.Context, providerName, redirectBase string) (url string, state string, err error)
	Callback(ctx context.Context, providerName, code, state string) (*auth.AuthResult, error)
	Close()
}

// mountOAuthRoutes registers the OAuth2 endpoints under /api.
func (s *Server) mountOAuthRoutes(api chi.Router) {
	api.Route("/v1/auth/oauth", func(r chi.Router) {
		r.Get("/{provider}/login", s.handleOAuthLogin)
		r.Get("/{provider}/callback", s.handleOAuthCallback)
	})
}

// handleOAuthLogin starts the authorization flow: it asks the service to mint a
// state, binds it to the browser with a cookie, and redirects to the provider.
// Unknown or disabled providers answer 404.
func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")

	url, state, err := s.oauth.Begin(r.Context(), provider, s.oauthRedirectBase())
	if err != nil {
		if errors.Is(err, auth.ErrProviderDisabled) {
			writeJSON(w, http.StatusNotFound, apiError{Message: "oauth provider not available"})
			return
		}
		s.logger.Error("oauth: begin", "provider", provider, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	http.SetCookie(w, oauthStateCookie(r, state, oauthStateCookieMaxAge))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOAuthCallback completes the flow. The state cookie must match the query
// state (constant-time); the service then resolves the account and returns a
// token pair, which is forwarded to the SPA in the URL fragment so it never
// reaches server logs. Every failure redirects to the login page with a generic
// error code.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	cookie, err := r.Cookie(auth.StateCookieName)
	if err != nil || queryState == "" || !constantTimeEqual(cookie.Value, queryState) {
		s.logger.Warn("oauth: state cookie mismatch", "provider", provider)
		s.redirectOAuthFailure(w, r)
		return
	}

	result, err := s.oauth.Callback(r.Context(), provider, code, queryState)
	if err != nil {
		s.logger.Warn("oauth: callback failed", "provider", provider, "error", err)
		s.redirectOAuthFailure(w, r)
		return
	}

	http.SetCookie(w, oauthStateCookie(r, "", -1))
	http.Redirect(w, r, s.oauthSuccessLocation(provider, result), http.StatusFound)
}

// oauthSuccessLocation builds the post-login redirect: an /oauth/callback route
// on the configured origin (or a relative path when no redirect URL is set) with
// the token pair in the fragment.
func (s *Server) oauthSuccessLocation(provider string, result *auth.AuthResult) string {
	fragment := url.Values{}
	fragment.Set("access_token", result.AccessToken)
	fragment.Set("refresh_token", result.RefreshToken)
	fragment.Set("expires_in", strconv.FormatInt(result.ExpiresIn, 10))

	return s.oauthRedirectBase() + oauthCallbackPath +
		"?provider=" + url.QueryEscape(provider) +
		"#" + fragment.Encode()
}

// oauthRedirectBase derives the post-login origin from the configured GitHub
// callback URL (scheme and host only). It returns "" when unconfigured, which
// makes the success redirect relative to the current origin.
func (s *Server) oauthRedirectBase() string {
	raw := s.cfg.Snapshot().OAuth.GitHub.RedirectURL
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// redirectOAuthFailure clears the state cookie and redirects to the login page.
func (s *Server) redirectOAuthFailure(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, oauthStateCookie(r, "", -1))
	http.Redirect(w, r, oauthFailureRedirect, http.StatusFound)
}

// oauthStateCookie builds the state cookie. It is HttpOnly, SameSite=Lax, and
// Secure only when the request is served over HTTPS so local HTTP development
// still works.
func oauthStateCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     auth.StateCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	}
}

// isSecureRequest reports whether the request reached us over HTTPS, directly or
// through a TLS-terminating proxy.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// constantTimeEqual compares two strings without leaking their contents through
// timing.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
