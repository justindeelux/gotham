package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

// fakeOAuthService is a deterministic OAuthService for handler tests.
type fakeOAuthService struct {
	callback    *auth.AuthResult
	callbackErr error
	closed      bool
}

func (f *fakeOAuthService) Begin(_ context.Context, providerName, _ string) (string, string, error) {
	if providerName != "github" {
		return "", "", auth.ErrProviderDisabled
	}
	return "https://github.example/authorize?state=test-state", "test-state", nil
}

func (f *fakeOAuthService) Callback(_ context.Context, _ string, _, _ string, _ auth.SessionMeta) (*auth.AuthResult, error) {
	if f.callbackErr != nil {
		return nil, f.callbackErr
	}
	return f.callback, nil
}

func (f *fakeOAuthService) Close() { f.closed = true }

// newOAuthTestServer builds a Server backed by the fake OAuth service and the
// given config.
func newOAuthTestServer(t *testing.T, cfg *config.Config, oauth OAuthService) *Server {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, nil, oauth, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s
}

// newOAuthTestServerWithAuth builds a Server with both the fake auth service
// and the OAuth service, so tests can exercise cookie clearing on the password
// flows and the exchange endpoint together.
func newOAuthTestServerWithAuth(t *testing.T, cfg *config.Config, oauth OAuthService) *Server {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, newFakeAuthService(), oauth, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s
}

// defaultOAuthConfig is the config used by OAuth handler tests: it opts into
// plain-HTTP development with an explicit http:// redirect base, so the
// insecure-request guard does not reject the HTTP tests (L1).
func defaultOAuthConfig() *config.Config {
	return &config.Config{
		Values: config.Values{
			Server: config.Server{Addr: "127.0.0.1", Port: 0},
			OAuth: config.OAuth{
				GitHub: config.OAuthGitHub{
					RedirectURL: "http://localhost:8000/api/v1/auth/oauth/github/callback",
				},
			},
		},
	}
}

// defaultOAuthResult is the token pair the fake callback returns.
func defaultOAuthResult() *auth.AuthResult {
	return &auth.AuthResult{
		User:         &auth.User{ID: testUserID.String(), Email: "user@example.com"},
		AccessToken:  "access-token",
		ExpiresIn:    900,
		RefreshToken: "refresh-token",
	}
}

// testOAuthFlow is the flow-binding cookie value shared by the exchange tests.
const testOAuthFlow = "flow-123"

// oauthRequest performs a GET against an OAuth route.
func oauthRequest(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// oauthCallbackRequest performs a callback GET with the given state and flow
// cookie values (empty values omit the cookie).
func oauthCallbackRequest(t *testing.T, s *Server, path, stateCookie, flowCookie string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if stateCookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.StateCookieName, Value: stateCookie})
	}
	if flowCookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.FlowCookieName, Value: flowCookie})
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// oauthExchangePost posts a one-time code, optionally with the flow cookie.
func oauthExchangePost(t *testing.T, s *Server, code, flowCookie string) *httptest.ResponseRecorder {
	t.Helper()

	body := strings.NewReader(`{"code":` + strconv.Quote(code) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	if flowCookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.FlowCookieName, Value: flowCookie})
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// exchangeCodeFromLocation extracts the one-time code from a callback redirect.
func exchangeCodeFromLocation(t *testing.T, location string) string {
	t.Helper()

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location %q: %v", location, err)
	}
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatalf("redirect %q carries no exchange code", location)
	}
	return code
}

// issueExchangeCode runs a successful callback for the fake and returns the
// one-time code it redirects with.
func issueExchangeCode(t *testing.T, s *Server) string {
	t.Helper()

	rec := oauthCallbackRequest(t, s,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code",
		"test-state", testOAuthFlow)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	return exchangeCodeFromLocation(t, rec.Header().Get("Location"))
}

// setCookieValue finds a Set-Cookie by name in a response.
func setCookieValue(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestOAuthLoginRedirectsToProvider(t *testing.T) {
	oauth := &fakeOAuthService{}
	s := newOAuthTestServer(t, defaultOAuthConfig(), oauth)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/login")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://github.example/authorize?state=test-state" {
		t.Errorf("Location = %q", loc)
	}

	state := setCookieValue(rec, auth.StateCookieName)
	if state == nil {
		t.Fatal("state cookie not set")
	}
	if state.Value != "test-state" {
		t.Errorf("state cookie value = %q, want test-state", state.Value)
	}
	if !state.HttpOnly {
		t.Error("state cookie is not HttpOnly")
	}
	if state.SameSite != http.SameSiteLaxMode {
		t.Errorf("state cookie SameSite = %v, want Lax", state.SameSite)
	}
	if state.Path != "/" {
		t.Errorf("state cookie Path = %q, want /", state.Path)
	}
	if state.Secure {
		t.Error("state cookie is Secure over plain HTTP")
	}
	if state.MaxAge != oauthStateCookieMaxAge {
		t.Errorf("state cookie MaxAge = %d, want %d", state.MaxAge, oauthStateCookieMaxAge)
	}

	flow := setCookieValue(rec, auth.FlowCookieName)
	if flow == nil {
		t.Fatal("flow cookie not set")
	}
	if flow.Value == "" {
		t.Error("flow cookie has an empty value")
	}
	if !flow.HttpOnly {
		t.Error("flow cookie is not HttpOnly")
	}
	if flow.SameSite != http.SameSiteLaxMode {
		t.Errorf("flow cookie SameSite = %v, want Lax", flow.SameSite)
	}
	if flow.Secure {
		t.Error("flow cookie is Secure over plain HTTP")
	}
	if flow.MaxAge != oauthFlowCookieMaxAge {
		t.Errorf("flow cookie MaxAge = %d, want %d", flow.MaxAge, oauthFlowCookieMaxAge)
	}
}

func TestOAuthLoginSecureCookieOverTLS(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/login", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	// Over HTTPS the cookies use the __Host- prefix, which requires Secure,
	// Path=/ and no Domain; a sibling subdomain therefore cannot shadow them.
	for _, name := range []string{auth.StateCookieNameSecure, auth.FlowCookieNameSecure} {
		cookie := setCookieValue(rec, name)
		if cookie == nil {
			t.Fatalf("%s cookie not set", name)
		}
		if !cookie.Secure {
			t.Errorf("%s cookie is not Secure behind an HTTPS proxy", name)
		}
		if cookie.Path != "/" {
			t.Errorf("%s cookie Path = %q, want /", name, cookie.Path)
		}
		if cookie.Domain != "" {
			t.Errorf("%s cookie Domain = %q, want empty (__Host- forbids it)", name, cookie.Domain)
		}
	}

	// The insecure plain names must not be set on a secure request.
	for _, name := range []string{auth.StateCookieName, auth.FlowCookieName} {
		if cookie := setCookieValue(rec, name); cookie != nil {
			t.Errorf("secure request set insecure cookie %s", name)
		}
	}
}

func TestOAuthLoginUnknownProvider(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/gitlab/login")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestOAuthRoutesAbsentWithoutService(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), nil)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/login")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestOAuthCallbackRedirectsWithCode(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	rec := oauthCallbackRequest(t, s,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code",
		"test-state", "flow-123")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "oauth_failed") {
		t.Fatalf("callback failed: %s", loc)
	}
	// The redirect must never carry the token pair: only a one-time code.
	for _, secret := range []string{"access-token", "refresh-token"} {
		if strings.Contains(loc, secret) {
			t.Errorf("Location %q leaks %q", loc, secret)
		}
	}

	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if parsed.Path != "/oauth/callback" {
		t.Errorf("redirect path = %q, want /oauth/callback", parsed.Path)
	}
	if parsed.Fragment != "" {
		t.Errorf("redirect fragment = %q, want empty", parsed.Fragment)
	}
	exchangeCodeFromLocation(t, loc)

	// The state cookie is cleared; the flow cookie is deliberately retained so
	// the SPA can redeem the code.
	state := setCookieValue(rec, auth.StateCookieName)
	if state == nil || state.MaxAge >= 0 {
		t.Errorf("state cookie not cleared: %+v", state)
	}
	if flow := setCookieValue(rec, auth.FlowCookieName); flow != nil {
		t.Errorf("callback rewrote the flow cookie: %+v", flow)
	}
}

func TestOAuthCallbackSuccessUsesConfiguredOrigin(t *testing.T) {
	cfg := defaultOAuthConfig()
	cfg.OAuth.GitHub.RedirectURL = "http://localhost:8000/api/v1/auth/oauth/github/callback"

	s := newOAuthTestServer(t, cfg, &fakeOAuthService{callback: defaultOAuthResult()})

	rec := oauthCallbackRequest(t, s,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=code",
		"test-state", "flow-123")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://localhost:8000/oauth/callback?code=") {
		t.Errorf("Location = %q, want configured-origin code redirect", loc)
	}
}

func TestOAuthCallbackRejectsBadState(t *testing.T) {
	tests := map[string]struct {
		path        string
		cookieValue string
	}{
		"missing cookie": {path: "/api/v1/auth/oauth/github/callback?state=test-state&code=code"},
		"mismatched cookie": {
			path:        "/api/v1/auth/oauth/github/callback?state=test-state&code=code",
			cookieValue: "other-state",
		},
		"missing query state": {
			path:        "/api/v1/auth/oauth/github/callback?code=code",
			cookieValue: "test-state",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{
				callback: &auth.AuthResult{AccessToken: "should-not-be-issued"},
			})

			rec := oauthCallbackRequest(t, s, tc.path, tc.cookieValue, "flow-123")

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "/login?error=oauth_failed" {
				t.Fatalf("Location = %q, want /login?error=oauth_failed", loc)
			}
		})
	}
}

func TestOAuthCallbackRejectsMissingFlowCookie(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	rec := oauthCallbackRequest(t, s,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=code",
		"test-state", "")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login?error=oauth_failed" {
		t.Fatalf("Location = %q, want /login?error=oauth_failed", loc)
	}
}

func TestOAuthCallbackProviderError(t *testing.T) {
	oauth := &fakeOAuthService{callbackErr: auth.ErrStateMismatch}
	s := newOAuthTestServer(t, defaultOAuthConfig(), oauth)

	rec := oauthCallbackRequest(t, s,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=code",
		"test-state", "flow-123")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/login?error=oauth_failed" {
		t.Fatalf("Location = %q, want /login?error=oauth_failed", loc)
	}
	if strings.Contains(loc, "state mismatch") {
		t.Error("error details leaked into the redirect query")
	}
}

func TestOAuthExchangeHappyPath(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	code := issueExchangeCode(t, s)

	rec := oauthExchangePost(t, s, code, "flow-123")
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode exchange body %q: %v", rec.Body.String(), err)
	}
	if body.AccessToken != "access-token" || body.RefreshToken != "refresh-token" {
		t.Errorf("exchange tokens = (%q, %q), want the callback pair", body.AccessToken, body.RefreshToken)
	}
	if body.User == nil || body.User.Email != "user@example.com" {
		t.Errorf("exchange user = %+v", body.User)
	}
	if body.TokenType != tokenTypeBearer {
		t.Errorf("token_type = %q, want %q", body.TokenType, tokenTypeBearer)
	}

	// The flow cookie is cleared once the code is redeemed.
	flow := setCookieValue(rec, auth.FlowCookieName)
	if flow == nil || flow.MaxAge >= 0 {
		t.Errorf("flow cookie not cleared: %+v", flow)
	}

	// A code is single-use.
	replay := oauthExchangePost(t, s, code, "flow-123")
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed exchange status = %d, want 401", replay.Code)
	}
}

func TestOAuthExchangeRejections(t *testing.T) {
	const flow = "flow-123"

	tests := map[string]struct {
		setup func(t *testing.T, s *Server) string
		flow  string
	}{
		"missing cookie": {
			setup: func(t *testing.T, s *Server) string { return issueExchangeCode(t, s) },
			flow:  "",
		},
		"wrong cookie": {
			setup: func(t *testing.T, s *Server) string { return issueExchangeCode(t, s) },
			flow:  "other-flow",
		},
		"unknown code": {
			setup: func(_ *testing.T, _ *Server) string { return "not-a-real-code" },
			flow:  flow,
		},
		"expired code": {
			setup: func(t *testing.T, s *Server) string {
				code := issueExchangeCode(t, s)
				s.oauthCodes.now = func() time.Time { return time.Now().Add(2 * oauthExchangeTTL) }
				return code
			},
			flow: flow,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})
			code := tc.setup(t, s)

			rec := oauthExchangePost(t, s, code, tc.flow)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}

			var body apiError
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode 401 body %q: %v", rec.Body.String(), err)
			}
			if body.Message != "unauthorized" {
				t.Errorf("401 message = %q, want a generic unauthorized", body.Message)
			}
		})
	}
}

func TestOAuthLoginRateLimit(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	got := []int{
		oauthRequest(t, s, "/api/v1/auth/oauth/github/login").Code,
		oauthRequest(t, s, "/api/v1/auth/oauth/github/login").Code,
		oauthRequest(t, s, "/api/v1/auth/oauth/github/login").Code,
	}
	want := []int{http.StatusFound, http.StatusFound, http.StatusTooManyRequests}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d status = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

func TestOAuthLoginNotFoundBodyIsJSON(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/gitlab/login")

	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 404 body %q: %v", rec.Body.String(), err)
	}
	if body.Message == "" {
		t.Error("404 body has an empty message")
	}
}

// TestOAuthCallbackRejectsDuplicateFlowCookie pins the HTTP-dev defense: a
// planted duplicate flow cookie must fail the read closed rather than win.
func TestOAuthCallbackRejectsDuplicateFlowCookie(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=code", nil)
	req.AddCookie(&http.Cookie{Name: auth.StateCookieName, Value: "test-state"})
	req.AddCookie(&http.Cookie{Name: auth.FlowCookieName, Value: testOAuthFlow})
	req.AddCookie(&http.Cookie{Name: auth.FlowCookieName, Value: "planted-flow"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != oauthFailureRedirect {
		t.Fatalf("Location = %q, want %s", loc, oauthFailureRedirect)
	}
}

// TestOAuthExchangeRejectsDuplicateFlowCookie pins the same defense on the
// exchange endpoint.
func TestOAuthExchangeRejectsDuplicateFlowCookie(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})
	code := issueExchangeCode(t, s)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/exchange",
		strings.NewReader(`{"code":`+strconv.Quote(code)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.FlowCookieName, Value: testOAuthFlow})
	req.AddCookie(&http.Cookie{Name: auth.FlowCookieName, Value: "planted-flow"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestOAuthExchangeRejectedAfterLogout proves logout clears the flow cookie so
// a pending exchange is no longer redeemable.
func TestOAuthExchangeRejectedAfterLogout(t *testing.T) {
	s := newOAuthTestServerWithAuth(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})
	code := issueExchangeCode(t, s)

	logout := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout",
		`{"refresh_token":"refresh-token"}`, "")
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logout.Code)
	}
	flow := setCookieValue(logout, auth.FlowCookieName)
	if flow == nil || flow.MaxAge >= 0 {
		t.Fatalf("logout did not clear the flow cookie: %+v", flow)
	}
	if state := setCookieValue(logout, auth.StateCookieName); state == nil || state.MaxAge >= 0 {
		t.Fatalf("logout did not clear the state cookie: %+v", state)
	}

	if rec := oauthExchangePost(t, s, code, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("exchange after logout status = %d, want 401", rec.Code)
	}
}

// TestOAuthExchangeRejectedAfterPasswordLogin proves a password login clears
// the flow cookie so a pending exchange cannot replace the new session.
func TestOAuthExchangeRejectedAfterPasswordLogin(t *testing.T) {
	s := newOAuthTestServerWithAuth(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})
	code := issueExchangeCode(t, s)

	login := doRequest(t, s, http.MethodPost, "/api/v1/auth/login",
		`{"email":"user@example.com","password":"password123"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", login.Code, login.Body.String())
	}
	flow := setCookieValue(login, auth.FlowCookieName)
	if flow == nil || flow.MaxAge >= 0 {
		t.Fatalf("login did not clear the flow cookie: %+v", flow)
	}

	if rec := oauthExchangePost(t, s, code, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("exchange after login status = %d, want 401", rec.Code)
	}
}

// TestOAuthExchangeRateLimit pins that the exchange endpoint shares the auth
// limiter.
func TestOAuthExchangeRateLimit(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	got := []int{
		oauthExchangePost(t, s, "unknown", testOAuthFlow).Code,
		oauthExchangePost(t, s, "unknown", testOAuthFlow).Code,
		oauthExchangePost(t, s, "unknown", testOAuthFlow).Code,
	}
	want := []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d status = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

// TestOAuthCodeStoreCleanup pins that the sweep drops expired codes.
func TestOAuthCodeStoreCleanup(t *testing.T) {
	store := newOAuthCodeStore()
	t.Cleanup(store.Close)

	base := time.Now()
	store.now = func() time.Time { return base }

	code, err := store.NewCode(defaultOAuthResult(), testOAuthFlow, false)
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}

	store.cleanup(base.Add(oauthExchangeTTL + time.Second))

	store.mu.Lock()
	_, present := store.entries[code]
	store.mu.Unlock()
	if present {
		t.Fatal("cleanup left an expired exchange code behind")
	}
}

// TestOAuthCodeStoreCapacity pins the exchange-code capacity cap.
func TestOAuthCodeStoreCapacity(t *testing.T) {
	store := newOAuthCodeStore()
	t.Cleanup(store.Close)

	for i := 0; i < oauthExchangeCapacity; i++ {
		if _, err := store.NewCode(defaultOAuthResult(), testOAuthFlow, false); err != nil {
			t.Fatalf("NewCode #%d: %v", i, err)
		}
	}

	if _, err := store.NewCode(defaultOAuthResult(), testOAuthFlow, false); !errors.Is(err, errOAuthCodeStoreFull) {
		t.Fatalf("NewCode past capacity error = %v, want errOAuthCodeStoreFull", err)
	}
}

// oauthRawRequest performs an OAuth request with an explicit scheme and cookies,
// for tests that exercise the HTTPS/HTTP cookie-name and scheme binding.
func oauthRawRequest(t *testing.T, s *Server, method, path, body string, secure bool, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if secure {
		req.TLS = &tls.ConnectionState{}
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestOAuthLoginRejectsInsecureFlowForHTTPSOrigin pins G1: an insecure request
// must not start a flow that returns to an HTTPS origin.
func TestOAuthLoginRejectsInsecureFlowForHTTPSOrigin(t *testing.T) {
	cfg := defaultOAuthConfig()
	cfg.OAuth.GitHub.RedirectURL = "https://gotham.example/api/v1/auth/oauth/github/callback"
	s := newOAuthTestServer(t, cfg, &fakeOAuthService{})

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/login")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != oauthFailureRedirect {
		t.Fatalf("Location = %q, want %s", loc, oauthFailureRedirect)
	}
}

// TestOAuthExchangeRejectsSchemeDowngrade pins G1: a code issued over HTTPS
// cannot be redeemed over plain HTTP, even with the matching binding value.
func TestOAuthExchangeRejectsSchemeDowngrade(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	callback := oauthRawRequest(t, s, http.MethodGet,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code", "", true,
		&http.Cookie{Name: auth.StateCookieNameSecure, Value: "test-state"},
		&http.Cookie{Name: auth.FlowCookieNameSecure, Value: testOAuthFlow})
	if callback.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", callback.Code)
	}
	code := exchangeCodeFromLocation(t, callback.Header().Get("Location"))

	downgrade := oauthExchangePost(t, s, code, testOAuthFlow)
	if downgrade.Code != http.StatusUnauthorized {
		t.Fatalf("downgraded exchange status = %d, want 401 (body %s)", downgrade.Code, downgrade.Body.String())
	}
}

// TestOAuthExchangeSecureFlow proves the HTTPS path works end to end with the
// __Host- cookie names.
func TestOAuthExchangeSecureFlow(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{callback: defaultOAuthResult()})

	login := oauthRawRequest(t, s, http.MethodGet, "/api/v1/auth/oauth/github/login", "", true)
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", login.Code)
	}
	flow := setCookieValue(login, auth.FlowCookieNameSecure)
	if flow == nil {
		t.Fatal("secure flow cookie not set")
	}

	callback := oauthRawRequest(t, s, http.MethodGet,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code", "", true,
		&http.Cookie{Name: auth.StateCookieNameSecure, Value: "test-state"},
		&http.Cookie{Name: auth.FlowCookieNameSecure, Value: flow.Value})
	code := exchangeCodeFromLocation(t, callback.Header().Get("Location"))

	exchange := oauthRawRequest(t, s, http.MethodPost, "/api/v1/auth/oauth/exchange",
		`{"code":`+strconv.Quote(code)+`}`, true,
		&http.Cookie{Name: auth.FlowCookieNameSecure, Value: flow.Value})
	if exchange.Code != http.StatusOK {
		t.Fatalf("secure exchange status = %d, want 200 (body %s)", exchange.Code, exchange.Body.String())
	}
}

// TestOAuthHTTPDevFlowEndToEnd proves the explicit http redirect-base path still
// works end to end over plain HTTP.
func TestOAuthHTTPDevFlowEndToEnd(t *testing.T) {
	cfg := defaultOAuthConfig()
	cfg.OAuth.GitHub.RedirectURL = "http://localhost:8000/api/v1/auth/oauth/github/callback"
	s := newOAuthTestServer(t, cfg, &fakeOAuthService{callback: defaultOAuthResult()})

	login := oauthRawRequest(t, s, http.MethodGet, "/api/v1/auth/oauth/github/login", "", false)
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302 (body %s)", login.Code, login.Body.String())
	}
	flow := setCookieValue(login, auth.FlowCookieName)
	if flow == nil {
		t.Fatal("plain flow cookie not set")
	}

	callback := oauthRawRequest(t, s, http.MethodGet,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code", "", false,
		&http.Cookie{Name: auth.StateCookieName, Value: "test-state"},
		&http.Cookie{Name: auth.FlowCookieName, Value: flow.Value})
	code := exchangeCodeFromLocation(t, callback.Header().Get("Location"))

	exchange := oauthRawRequest(t, s, http.MethodPost, "/api/v1/auth/oauth/exchange",
		`{"code":`+strconv.Quote(code)+`}`, false,
		&http.Cookie{Name: auth.FlowCookieName, Value: flow.Value})
	if exchange.Code != http.StatusOK {
		t.Fatalf("HTTP dev exchange status = %d, want 200 (body %s)", exchange.Code, exchange.Body.String())
	}
}

// TestOAuthLogoutClearsBothCookieNameSets pins G2: an HTTPS logout clears the
// plain HTTP pair too (and vice versa).
func TestOAuthLogoutClearsBothCookieNameSets(t *testing.T) {
	s := newOAuthTestServerWithAuth(t, defaultOAuthConfig(), &fakeOAuthService{})

	// An HTTP login leaves the plain pair in the browser.
	login := oauthRawRequest(t, s, http.MethodGet, "/api/v1/auth/oauth/github/login", "", false)
	if setCookieValue(login, auth.FlowCookieName) == nil {
		t.Fatal("plain flow cookie not set")
	}

	// An HTTPS logout must clear both name sets.
	logout := oauthRawRequest(t, s, http.MethodPost, "/api/v1/auth/logout",
		`{"refresh_token":"refresh-token"}`, true)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logout.Code)
	}
	for _, name := range []string{
		auth.StateCookieName,
		auth.FlowCookieName,
		auth.StateCookieNameSecure,
		auth.FlowCookieNameSecure,
	} {
		cookie := setCookieValue(logout, name)
		if cookie == nil || cookie.MaxAge >= 0 {
			t.Errorf("%s not cleared: %+v", name, cookie)
		}
	}
	if cookie := setCookieValue(logout, auth.FlowCookieNameSecure); cookie != nil && !cookie.Secure {
		t.Error("__Host- deletion is not Secure")
	}
}

// TestOAuthCallbackRejectsInsecureRequestForHTTPSOrigin pins H1: an HTTP
// callback on an HTTPS deployment is refused before any cookie or code is
// trusted.
func TestOAuthCallbackRejectsInsecureRequestForHTTPSOrigin(t *testing.T) {
	cfg := defaultOAuthConfig()
	cfg.OAuth.GitHub.RedirectURL = "https://gotham.example/api/v1/auth/oauth/github/callback"
	s := newOAuthTestServer(t, cfg, &fakeOAuthService{callback: defaultOAuthResult()})

	rec := oauthRawRequest(t, s, http.MethodGet,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code", "", false,
		&http.Cookie{Name: auth.StateCookieName, Value: "test-state"},
		&http.Cookie{Name: auth.FlowCookieName, Value: testOAuthFlow})

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != oauthFailureRedirect {
		t.Fatalf("Location = %q, want %s", loc, oauthFailureRedirect)
	}
	if strings.Contains(loc, "code=") {
		t.Errorf("rejected callback leaked an exchange code: %s", loc)
	}
}

// TestOAuthExchangeRejectsInsecureRequestForHTTPSOrigin pins H1: an HTTP
// redemption on an HTTPS deployment is refused even for a code minted over
// HTTPS.
func TestOAuthExchangeRejectsInsecureRequestForHTTPSOrigin(t *testing.T) {
	cfg := defaultOAuthConfig()
	cfg.OAuth.GitHub.RedirectURL = "https://gotham.example/api/v1/auth/oauth/github/callback"
	s := newOAuthTestServer(t, cfg, &fakeOAuthService{callback: defaultOAuthResult()})

	callback := oauthRawRequest(t, s, http.MethodGet,
		"/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code", "", true,
		&http.Cookie{Name: auth.StateCookieNameSecure, Value: "test-state"},
		&http.Cookie{Name: auth.FlowCookieNameSecure, Value: testOAuthFlow})
	code := exchangeCodeFromLocation(t, callback.Header().Get("Location"))

	rec := oauthExchangePost(t, s, code, testOAuthFlow)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// The rejected attempt must not have consumed the code: the same code still
	// redeems over HTTPS.
	allowed := oauthRawRequest(t, s, http.MethodPost, "/api/v1/auth/oauth/exchange",
		`{"code":`+strconv.Quote(code)+`}`, true,
		&http.Cookie{Name: auth.FlowCookieNameSecure, Value: testOAuthFlow})
	if allowed.Code != http.StatusOK {
		t.Fatalf("secure exchange of the same code = %d, want 200 (body %s)", allowed.Code, allowed.Body.String())
	}
}

// TestOAuthEmptyRedirectBaseRequiresHTTPS pins L1: with no usable redirect base
// the guard fails closed for insecure requests, while a secure request is
// allowed.
func TestOAuthEmptyRedirectBaseRequiresHTTPS(t *testing.T) {
	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	s := newOAuthTestServer(t, cfg, &fakeOAuthService{})

	insecure := oauthRequest(t, s, "/api/v1/auth/oauth/github/login")
	if insecure.Code != http.StatusFound {
		t.Fatalf("insecure login status = %d, want 302", insecure.Code)
	}
	if loc := insecure.Header().Get("Location"); loc != oauthFailureRedirect {
		t.Fatalf("insecure login Location = %q, want %s", loc, oauthFailureRedirect)
	}

	secure := oauthRawRequest(t, s, http.MethodGet, "/api/v1/auth/oauth/github/login", "", true)
	if secure.Code != http.StatusFound {
		t.Fatalf("secure login status = %d, want 302", secure.Code)
	}
	if loc := secure.Header().Get("Location"); loc == oauthFailureRedirect {
		t.Fatalf("secure login was rejected with the empty base: %s", loc)
	}
}
