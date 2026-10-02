package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/auth"
)

// oauthStateCookieMaxAge is the state cookie lifetime in seconds. It matches the
// server-side state TTL.
const oauthStateCookieMaxAge = 600

// oauthFlowCookieMaxAge is the flow-binding cookie lifetime in seconds. It
// matches the state cookie so the binding survives the provider round-trip.
const oauthFlowCookieMaxAge = 600

// oauthRandomBytes is the entropy of the flow binding and the one-time
// exchange code.
const oauthRandomBytes = 32

// One-time exchange-code bookkeeping. A code is single-use and expires after
// oauthExchangeTTL; expired entries are swept by a background goroutine.
const (
	oauthExchangeTTL           = 60 * time.Second
	oauthExchangeCleanupPeriod = time.Minute
)

// oauthFailureRedirect is where a failed callback sends the browser. The query
// never carries error details so nothing is leaked to logs or referrers.
const oauthFailureRedirect = "/login?error=oauth_failed"

// oauthCallbackPath is the SPA route that redeems the one-time exchange code
// after a successful callback.
const oauthCallbackPath = "/oauth/callback"

// oauthExchangeRequest is the body of the exchange endpoint.
type oauthExchangeRequest struct {
	Code string `json:"code"`
}

// OAuthService is the subset of auth.OAuthService the HTTP layer depends on.
// Keeping it an interface lets tests substitute a fake without a database or a
// real provider.
type OAuthService interface {
	Begin(ctx context.Context, providerName, redirectBase string) (url string, state string, err error)
	Callback(ctx context.Context, providerName, code, state string) (*auth.AuthResult, error)
	Close()
}

// mountOAuthRoutes registers the OAuth2 endpoints under /api. The login redirect
// (which allocates server-side state) and the exchange endpoint are rate
// limited; the callback is not, because the provider drives it.
func (s *Server) mountOAuthRoutes(api chi.Router) {
	api.Route("/v1/auth/oauth", func(r chi.Router) {
		r.With(s.rateLimit).Get("/{provider}/login", s.handleOAuthLogin)
		r.Get("/{provider}/callback", s.handleOAuthCallback)
		r.With(s.rateLimit).Post("/exchange", s.handleOAuthExchange)
	})
}

// handleOAuthLogin starts the authorization flow: it asks the service to mint a
// state, binds the browser to the flow with a state cookie and a random flow
// cookie, and redirects to the provider. Unknown or disabled providers answer
// 404.
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

	binding, err := newOAuthFlowBinding()
	if err != nil {
		s.logger.Error("oauth: flow binding", "provider", provider, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	http.SetCookie(w, oauthStateCookie(r, state, oauthStateCookieMaxAge))
	http.SetCookie(w, oauthFlowCookie(r, binding, oauthFlowCookieMaxAge))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOAuthCallback completes the flow. The state cookie must match the query
// state (constant-time) and the flow cookie must be present; the service then
// resolves the account and returns a token pair. Instead of forwarding the
// tokens, the handler stores them behind a random one-time code bound to the
// flow cookie and redirects the SPA to redeem that code, so a crafted callback
// link cannot plant a session. Every failure redirects to the login page with a
// generic error code.
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

	flow, err := r.Cookie(auth.FlowCookieName)
	if err != nil || flow.Value == "" {
		s.logger.Warn("oauth: flow cookie missing", "provider", provider)
		s.redirectOAuthFailure(w, r)
		return
	}

	result, err := s.oauth.Callback(r.Context(), provider, code, queryState)
	if err != nil {
		s.logger.Warn("oauth: callback failed", "provider", provider, "error", err)
		s.redirectOAuthFailure(w, r)
		return
	}

	exchangeCode, err := s.oauthCodes.NewCode(result, flow.Value)
	if err != nil {
		s.logger.Error("oauth: issue exchange code", "provider", provider, "error", err)
		s.redirectOAuthFailure(w, r)
		return
	}

	// The flow cookie stays: the SPA needs it to redeem the code.
	http.SetCookie(w, oauthStateCookie(r, "", -1))
	http.Redirect(w, r, s.oauthSuccessLocation(exchangeCode), http.StatusFound)
}

// handleOAuthExchange redeems a one-time code for the token pair that the
// callback withheld. The flow cookie must match the value stored with the code;
// a missing, wrong, expired, or already-redeemed code answers a generic 401.
func (s *Server) handleOAuthExchange(w http.ResponseWriter, r *http.Request) {
	var req oauthExchangeRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	flow, err := r.Cookie(auth.FlowCookieName)
	if err != nil || flow.Value == "" {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	result, ok := s.oauthCodes.Exchange(req.Code, flow.Value)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	http.SetCookie(w, oauthFlowCookie(r, "", -1))
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

// oauthSuccessLocation builds the post-login redirect: an /oauth/callback route
// on the configured origin (or a relative path when no redirect URL is set)
// carrying the one-time exchange code.
func (s *Server) oauthSuccessLocation(code string) string {
	return s.oauthRedirectBase() + oauthCallbackPath + "?code=" + url.QueryEscape(code)
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

// redirectOAuthFailure clears both OAuth cookies and redirects to the login
// page.
func (s *Server) redirectOAuthFailure(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, oauthStateCookie(r, "", -1))
	http.SetCookie(w, oauthFlowCookie(r, "", -1))
	http.Redirect(w, r, oauthFailureRedirect, http.StatusFound)
}

// newOAuthFlowBinding mints a random value binding the browser that started the
// flow to the exchange that redeems it.
func newOAuthFlowBinding() (string, error) {
	buf := make([]byte, oauthRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth: generate flow binding: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// oauthStateCookie builds the state cookie. It is HttpOnly, SameSite=Lax, and
// Secure only when the request is served over HTTPS so local HTTP development
// still works.
func oauthStateCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return oauthCookie(r, auth.StateCookieName, value, maxAge)
}

// oauthFlowCookie builds the flow-binding cookie. It carries the same
// protections as the state cookie.
func oauthFlowCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return oauthCookie(r, auth.FlowCookieName, value, maxAge)
}

// oauthCookie builds an OAuth protocol cookie: HttpOnly, SameSite=Lax, and
// Secure only when the request is served over HTTPS so local HTTP development
// still works.
func oauthCookie(r *http.Request, name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
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

// oauthCodeStore holds pending exchange codes in memory. Entries are single-use
// and expire after oauthExchangeTTL; expired entries are swept by a background
// goroutine. It mirrors the auth package's OAuth state store.
type oauthCodeStore struct {
	mu      sync.Mutex
	entries map[string]oauthExchangeEntry
	now     func() time.Time
	stop    chan struct{}
	once    sync.Once
}

// oauthExchangeEntry is one callback result waiting to be claimed by the browser
// that started the flow.
type oauthExchangeEntry struct {
	result    *auth.AuthResult
	binding   string
	expiresAt time.Time
}

// newOAuthCodeStore builds a store and starts its cleanup goroutine. Callers
// must Close it to stop that goroutine.
func newOAuthCodeStore() *oauthCodeStore {
	s := &oauthCodeStore{
		entries: make(map[string]oauthExchangeEntry),
		now:     time.Now,
		stop:    make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// NewCode mints a random one-time code bound to binding and remembers result
// until TTL expiry.
func (s *oauthCodeStore) NewCode(result *auth.AuthResult, binding string) (string, error) {
	buf := make([]byte, oauthRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth: generate exchange code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	s.entries[code] = oauthExchangeEntry{
		result:    result,
		binding:   binding,
		expiresAt: s.now().Add(oauthExchangeTTL),
	}
	s.mu.Unlock()

	return code, nil
}

// Exchange consumes code and returns the token pair only when binding matches
// the value stored at callback time (constant-time). It reports ok=false for an
// unknown, already-consumed, expired, or foreign code.
func (s *oauthCodeStore) Exchange(code, binding string) (*auth.AuthResult, bool) {
	s.mu.Lock()
	entry, ok := s.entries[code]
	if ok {
		delete(s.entries, code)
	}
	s.mu.Unlock()

	if !ok {
		return nil, false
	}
	if s.now().After(entry.expiresAt) {
		return nil, false
	}
	if !constantTimeEqual(entry.binding, binding) {
		return nil, false
	}
	return entry.result, true
}

// cleanupLoop evicts expired codes until Close is called.
func (s *oauthCodeStore) cleanupLoop() {
	ticker := time.NewTicker(oauthExchangeCleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.cleanup(now)
		}
	}
}

// cleanup drops codes whose expiry has passed.
func (s *oauthCodeStore) cleanup(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for code, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, code)
		}
	}
}

// Close stops the cleanup goroutine. It is safe to call more than once.
func (s *oauthCodeStore) Close() {
	s.once.Do(func() { close(s.stop) })
}
