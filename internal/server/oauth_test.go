package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
	s, err := New(cfg, logger, nil, oauth, nil, nil)
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

// oauthRequest performs a request, optionally attaching the state cookie.
func oauthRequest(t *testing.T, s *Server, path, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: auth.StateCookieName, Value: cookieValue})
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestOAuthLoginRedirectsToProvider(t *testing.T) {
	oauth := &fakeOAuthService{}
	s := newOAuthTestServer(t, defaultOAuthConfig(), oauth)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/login", "")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://github.example/authorize?state=test-state" {
		t.Errorf("Location = %q", loc)
	}

	cookies := rec.Result().Cookies()
	var state *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.StateCookieName {
			state = c
		}
	}
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
}

func TestOAuthLoginSecureCookieOverTLS(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.StateCookieName && !c.Secure {
			t.Error("state cookie is not Secure behind an HTTPS proxy")
		}
	}
}

func TestOAuthLoginUnknownProvider(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/gitlab/login", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestOAuthRoutesAbsentWithoutService(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), nil)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/login", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestOAuthCallbackSuccessFragment(t *testing.T) {
	oauth := &fakeOAuthService{callback: &auth.AuthResult{
		User:         &auth.User{ID: testUserID.String(), Email: "user@example.com"},
		AccessToken:  "access-token",
		ExpiresIn:    900,
		RefreshToken: "refresh-token",
	}}
	s := newOAuthTestServer(t, defaultOAuthConfig(), oauth)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/callback?state=test-state&code=auth-code", "test-state")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "oauth_failed") {
		t.Fatalf("callback failed: %s", loc)
	}

	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if parsed.Path != "/oauth/callback" {
		t.Errorf("redirect path = %q, want /oauth/callback", parsed.Path)
	}
	if got := parsed.Query().Get("provider"); got != "github" {
		t.Errorf("provider = %q, want github", got)
	}

	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatalf("parse fragment %q: %v", parsed.Fragment, err)
	}
	if got := fragment.Get("access_token"); got != "access-token" {
		t.Errorf("fragment access_token = %q, want access-token", got)
	}
	if got := fragment.Get("refresh_token"); got != "refresh-token" {
		t.Errorf("fragment refresh_token = %q", got)
	}
	if got := fragment.Get("expires_in"); got != "900" {
		t.Errorf("fragment expires_in = %q, want 900", got)
	}

	// The state cookie must be cleared after use.
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.StateCookieName && c.MaxAge >= 0 {
			t.Errorf("state cookie not cleared: MaxAge = %d", c.MaxAge)
		}
	}
}

func TestOAuthCallbackSuccessUsesConfiguredOrigin(t *testing.T) {
	cfg := defaultOAuthConfig()
	cfg.OAuth.GitHub.RedirectURL = "http://localhost:8000/api/v1/auth/oauth/github/callback"

	oauth := &fakeOAuthService{callback: &auth.AuthResult{
		User: &auth.User{ID: testUserID.String(), Email: "user@example.com"},
	}}
	s := newOAuthTestServer(t, cfg, oauth)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/callback?state=test-state&code=code", "test-state")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://localhost:8000/oauth/callback?provider=github#") {
		t.Errorf("Location = %q, want configured-origin fragment redirect", loc)
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

			rec := oauthRequest(t, s, tc.path, tc.cookieValue)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "/login?error=oauth_failed" {
				t.Fatalf("Location = %q, want /login?error=oauth_failed", loc)
			}
		})
	}
}

func TestOAuthCallbackProviderError(t *testing.T) {
	oauth := &fakeOAuthService{callbackErr: auth.ErrStateMismatch}
	s := newOAuthTestServer(t, defaultOAuthConfig(), oauth)

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/github/callback?state=test-state&code=code", "test-state")

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

func TestOAuthLoginNotFoundBodyIsJSON(t *testing.T) {
	s := newOAuthTestServer(t, defaultOAuthConfig(), &fakeOAuthService{})

	rec := oauthRequest(t, s, "/api/v1/auth/oauth/gitlab/login", "")

	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 404 body %q: %v", rec.Body.String(), err)
	}
	if body.Message == "" {
		t.Error("404 body has an empty message")
	}
}
