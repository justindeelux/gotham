package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

// testUserID is the account the fake auth service authenticates.
var testUserID = uuid.MustParse("11111111-2222-3333-4444-555555555555")

// fakeAuthService is a deterministic AuthService for handler tests.
type fakeAuthService struct {
	user *auth.User
}

func newFakeAuthService() *fakeAuthService {
	return &fakeAuthService{
		user: &auth.User{ID: testUserID.String(), Email: "user@example.com"},
	}
}

func (f *fakeAuthService) result() *auth.AuthResult {
	return &auth.AuthResult{
		User:         f.user,
		AccessToken:  "access-token",
		ExpiresIn:    900,
		RefreshToken: "refresh-token",
	}
}

func (f *fakeAuthService) Register(_ context.Context, email string, _ string) (*auth.AuthResult, error) {
	switch email {
	case "taken@example.com":
		return nil, auth.ErrEmailTaken
	case "invalid@example.com":
		return nil, auth.ErrValidation
	default:
		return f.result(), nil
	}
}

func (f *fakeAuthService) Login(_ context.Context, _ string, password string) (*auth.AuthResult, error) {
	if password == "wrong-password" {
		return nil, auth.ErrInvalidCredentials
	}
	return f.result(), nil
}

func (f *fakeAuthService) Refresh(_ context.Context, refreshToken string) (*auth.AuthResult, error) {
	if refreshToken != "refresh-token" {
		return nil, auth.ErrUnauthorized
	}
	return f.result(), nil
}

func (f *fakeAuthService) Logout(_ context.Context, _ string) error {
	return nil
}

func (f *fakeAuthService) Me(_ context.Context, userID uuid.UUID) (*auth.User, error) {
	if userID != testUserID {
		return nil, auth.ErrUnauthorized
	}
	return f.user, nil
}

func (f *fakeAuthService) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token != "valid-token" {
		return nil, auth.ErrInvalidToken
	}
	return &auth.Claims{
		Role:             "user",
		RegisteredClaims: jwt.RegisteredClaims{Subject: testUserID.String()},
	}, nil
}

// newTestAuthServer builds a Server backed by the fake auth service.
func newTestAuthServer(t *testing.T) *Server {
	t.Helper()

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s, err := New(cfg, logger, newFakeAuthService(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s
}

// doRequest performs a request against the server handler.
func doRequest(t *testing.T, s *Server, method, path, body, authorization string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAuthRegisterLogin(t *testing.T) {
	s := newTestAuthServer(t)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/register",
		`{"email":"new@example.com","password":"password123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var registered authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &registered); err != nil {
		t.Fatalf("decode register body: %v", err)
	}
	if registered.TokenType != tokenTypeBearer {
		t.Errorf("token_type = %q, want %q", registered.TokenType, tokenTypeBearer)
	}
	if registered.AccessToken == "" || registered.RefreshToken == "" {
		t.Errorf("register body missing tokens: %+v", registered)
	}
	if registered.User == nil || registered.User.Email != "user@example.com" {
		t.Errorf("register body user = %+v", registered.User)
	}

	login := doRequest(t, s, http.MethodPost, "/api/v1/auth/login",
		`{"email":"new@example.com","password":"password123"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", login.Code, login.Body.String())
	}
}

func TestAuthRegisterErrors(t *testing.T) {
	s := newTestAuthServer(t)

	conflict := doRequest(t, s, http.MethodPost, "/api/v1/auth/register",
		`{"email":"taken@example.com","password":"password123"}`, "")
	if conflict.Code != http.StatusConflict {
		t.Errorf("duplicate register status = %d, want 409", conflict.Code)
	}

	invalid := doRequest(t, s, http.MethodPost, "/api/v1/auth/register",
		`{"email":"invalid@example.com","password":"password123"}`, "")
	if invalid.Code != http.StatusBadRequest {
		t.Errorf("invalid register status = %d, want 400", invalid.Code)
	}

	malformed := doRequest(t, s, http.MethodPost, "/api/v1/auth/register", `{`, "")
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed register status = %d, want 400", malformed.Code)
	}
}

func TestAuthLoginRejectsBadCredentials(t *testing.T) {
	s := newTestAuthServer(t)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/login",
		`{"email":"user@example.com","password":"wrong-password"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAuthRefreshAndLogout(t *testing.T) {
	s := newTestAuthServer(t)

	ok := doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh",
		`{"refresh_token":"refresh-token"}`, "")
	if ok.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (body %s)", ok.Code, ok.Body.String())
	}

	bad := doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh",
		`{"refresh_token":"revoked-token"}`, "")
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad refresh status = %d, want 401", bad.Code)
	}

	logout := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout",
		`{"refresh_token":"refresh-token"}`, "")
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logout.Code)
	}

	empty := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout", "", "")
	if empty.Code != http.StatusNoContent {
		t.Fatalf("empty logout status = %d, want 204", empty.Code)
	}
}

func TestAuthMe(t *testing.T) {
	s := newTestAuthServer(t)

	authorized := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer valid-token")
	if authorized.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (body %s)", authorized.Code, authorized.Body.String())
	}

	var body meResponse
	if err := json.Unmarshal(authorized.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me body: %v", err)
	}
	if body.User == nil || body.User.ID != testUserID.String() {
		t.Errorf("me body user = %+v", body.User)
	}

	missing := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "")
	if missing.Code != http.StatusUnauthorized {
		t.Errorf("me without token status = %d, want 401", missing.Code)
	}

	bad := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer not-a-valid-token")
	if bad.Code != http.StatusUnauthorized {
		t.Errorf("me with bad token status = %d, want 401", bad.Code)
	}

	scheme := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Token valid-token")
	if scheme.Code != http.StatusUnauthorized {
		t.Errorf("me with wrong scheme status = %d, want 401", scheme.Code)
	}
}

func TestAuthRateLimit(t *testing.T) {
	s := newTestAuthServer(t)

	// Exhaust the bucket: the first burst requests pass, the next is limited.
	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	body := `{"email":"new@example.com","password":"password123"}`
	got := []int{
		doRequest(t, s, http.MethodPost, "/api/v1/auth/register", body, "").Code,
		doRequest(t, s, http.MethodPost, "/api/v1/auth/register", body, "").Code,
		doRequest(t, s, http.MethodPost, "/api/v1/auth/register", body, "").Code,
	}
	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d status = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestAuthServer(t)

	rec := doRequest(t, s, http.MethodGet, "/healthz", "", "")

	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": contentSecurityPolicy,
	}
	for name, want := range headers {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
