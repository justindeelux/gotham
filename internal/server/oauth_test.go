package server

import (
	"context"
	"encoding/json"
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

func (f *fakeOAuthService) Callback(_ context.Context, _ string, _, _ string) (*auth.AuthResult, error) {
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

// defaultOAuthConfig is the minimal config used by OAuth handler tests.
func defaultOAuthConfig() *config.Config {
	return &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
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
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	for _, name := range []string{auth.StateCookieName, auth.FlowCookieName} {
		cookie := setCookieValue(rec, name)
		if cookie == nil {
			t.Fatalf("%s cookie not set", name)
		}
		if !cookie.Secure {
			t.Errorf("%s cookie is not Secure behind an HTTPS proxy", name)
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
