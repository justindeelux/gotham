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
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

var (
	testUserID  = uuid.MustParse("11111111-2222-3333-4444-555555555555")
	testOwnerID = uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa")
)

type fakeAuthService struct {
	user              *auth.User
	users             map[uuid.UUID]*auth.User
	logoutErr         error
	totpEnabled       bool
	recoveryCodesLeft int
	mfaLogin          bool
}

func newFakeAuthService() *fakeAuthService {
	owner := &auth.User{ID: testOwnerID.String(), Email: "owner@example.com", Role: auth.RoleOwner}
	return &fakeAuthService{
		user:  &auth.User{ID: testUserID.String(), Email: "user@example.com", Role: auth.RoleMember},
		users: map[uuid.UUID]*auth.User{testOwnerID: owner},
	}
}

func ownerAuthService() *fakeAuthService {
	svc := newFakeAuthService()
	svc.user.Role = auth.RoleOwner
	return svc
}

func (f *fakeAuthService) result() *auth.AuthResult {
	return &auth.AuthResult{
		User:         f.user,
		AccessToken:  "access-token",
		ExpiresIn:    900,
		RefreshToken: "refresh-token",
	}
}

func (f *fakeAuthService) Register(_ context.Context, email string, _ string, _ auth.SessionMeta) (*auth.AuthResult, error) {
	switch email {
	case "taken@example.com":
		return nil, auth.ErrEmailTaken
	case "invalid@example.com":
		return nil, auth.ErrValidation
	case "closed@example.com":
		return nil, auth.ErrRegistrationClosed
	default:
		return f.result(), nil
	}
}

func (f *fakeAuthService) Login(_ context.Context, _ string, password string, _ auth.SessionMeta) (*auth.AuthResult, error) {
	if password == "wrong-password" {
		return nil, auth.ErrInvalidCredentials
	}
	if f.mfaLogin {
		return &auth.AuthResult{User: f.user, MFARequired: true, MFAToken: "mfa-token"}, nil
	}
	return f.result(), nil
}

func (f *fakeAuthService) Refresh(_ context.Context, refreshToken string, _ auth.SessionMeta) (*auth.AuthResult, error) {
	if refreshToken != "refresh-token" {
		return nil, auth.ErrUnauthorized
	}
	return f.result(), nil
}

func (f *fakeAuthService) Logout(_ context.Context, _ string) error {
	return f.logoutErr
}

func (f *fakeAuthService) Me(_ context.Context, userID uuid.UUID) (*auth.User, error) {
	if f.user != nil && f.user.ID == userID.String() {
		return f.user, nil
	}
	if user, ok := f.users[userID]; ok {
		return user, nil
	}
	return nil, auth.ErrUnauthorized
}

func (f *fakeAuthService) UpdateProfile(_ context.Context, userID uuid.UUID, displayName *string) (*auth.User, error) {
	if userID != testUserID {
		return nil, auth.ErrUnauthorized
	}
	if displayName != nil && len(*displayName) > 64 {
		return nil, auth.ErrDisplayNameInvalid
	}
	out := *f.user
	out.DisplayName = displayName
	return &out, nil
}

func (f *fakeAuthService) ChangePassword(_ context.Context, userID uuid.UUID, current, _ string, _ auth.SessionMeta) (*auth.AuthResult, error) {
	if userID != testUserID {
		return nil, auth.ErrUnauthorized
	}
	if current == "wrong-password" {
		return nil, auth.ErrCurrentPasswordIncorrect
	}
	return f.result(), nil
}

var fakeSessions = []auth.SessionInfo{
	{ID: uuid.MustParse("22222222-2222-3333-4444-555555555555"), UserAgent: "test-agent", IP: "10.0.0.1", Current: true},
	{ID: uuid.MustParse("33333333-2222-3333-4444-555555555555"), UserAgent: "other-agent", IP: "10.0.0.2"},
}

func (f *fakeAuthService) ListSessions(_ context.Context, userID, currentSessionID uuid.UUID) ([]auth.SessionInfo, error) {
	if userID != testUserID {
		return nil, auth.ErrUnauthorized
	}
	out := make([]auth.SessionInfo, 0, len(fakeSessions))
	for _, session := range fakeSessions {
		session.Current = session.ID == currentSessionID
		out = append(out, session)
	}
	return out, nil
}

func (f *fakeAuthService) SessionSignInAt(_ context.Context, userID, sessionID uuid.UUID) (time.Time, bool, error) {
	if userID != testUserID {
		return time.Time{}, false, auth.ErrUnauthorized
	}
	for _, session := range fakeSessions {
		if session.ID == sessionID {
			return session.CreatedAt, true, nil
		}
	}
	return time.Time{}, false, nil
}

func (f *fakeAuthService) RevokeSession(_ context.Context, userID, id uuid.UUID) error {
	if userID != testUserID {
		return auth.ErrUnauthorized
	}
	for _, session := range fakeSessions {
		if session.ID == id {
			return nil
		}
	}
	return auth.ErrNotFound
}

func (f *fakeAuthService) RevokeOtherSessions(_ context.Context, userID, currentSessionID uuid.UUID) error {
	if userID != testUserID {
		return auth.ErrUnauthorized
	}
	if currentSessionID == uuid.Nil {
		return auth.ErrSessionUnknown
	}
	return nil
}

func (f *fakeAuthService) VerifyAccessToken(token string) (*auth.Claims, error) {
	switch token {
	case "valid-token":
		return &auth.Claims{
			Role:             auth.RoleMember,
			RegisteredClaims: jwt.RegisteredClaims{Subject: testUserID.String()},
		}, nil
	case "sid-token":
		return &auth.Claims{
			Role:             auth.RoleMember,
			SessionID:        fakeSessions[0].ID.String(),
			RegisteredClaims: jwt.RegisteredClaims{Subject: testUserID.String()},
		}, nil
	case "owner-token":
		return &auth.Claims{
			Role:             auth.RoleOwner,
			RegisteredClaims: jwt.RegisteredClaims{Subject: testOwnerID.String()},
		}, nil
	default:
		return nil, auth.ErrInvalidToken
	}
}

func (f *fakeAuthService) TOTPStatus(_ context.Context, _ uuid.UUID) (*auth.TOTPStatus, error) {
	return &auth.TOTPStatus{Enabled: f.totpEnabled, RecoveryCodesRemaining: f.recoveryCodesLeft}, nil
}

func (f *fakeAuthService) BeginTOTPEnrollment(_ context.Context, _ uuid.UUID) (*auth.TOTPEnrollment, error) {
	return &auth.TOTPEnrollment{Secret: "JBSWY3DPEHPK3PXP", OTPAuthURL: "otpauth://totp/Gotham:user@example.com?secret=JBSWY3DPEHPK3PXP"}, nil
}

func (f *fakeAuthService) EnableTOTP(_ context.Context, _ uuid.UUID, password, code string, _ uuid.UUID) ([]string, error) {
	if password == "wrong-password" {
		return nil, auth.ErrCurrentPasswordIncorrect
	}
	if code != "123456" {
		return nil, auth.ErrInvalidCode
	}
	return []string{"ABCDE-FGHJK"}, nil
}

func (f *fakeAuthService) DisableTOTP(_ context.Context, _ uuid.UUID, password, code string, _ uuid.UUID) error {
	if password == "wrong-password" {
		return auth.ErrCurrentPasswordIncorrect
	}
	if code == "locked" {
		return auth.ErrTOTPLocked
	}
	if code != "123456" {
		return auth.ErrInvalidCode
	}
	return nil
}

func (f *fakeAuthService) RegenerateTOTPRecoveryCodes(_ context.Context, _ uuid.UUID, password, code string, _ uuid.UUID) ([]string, error) {
	if password == "wrong-password" {
		return nil, auth.ErrCurrentPasswordIncorrect
	}
	if code != "123456" {
		return nil, auth.ErrInvalidCode
	}
	return []string{"ABCDE-FGHJK"}, nil
}

func (f *fakeAuthService) CompleteMFALogin(_ context.Context, mfaToken, code string, _ auth.SessionMeta) (*auth.AuthResult, error) {
	if mfaToken != "mfa-token" {
		return nil, auth.ErrInvalidCredentials
	}
	if code == "locked" {
		return nil, auth.ErrTOTPLocked
	}
	if code != "123456" {
		return nil, auth.ErrInvalidCode
	}
	return f.result(), nil
}

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

	closed := doRequest(t, s, http.MethodPost, "/api/v1/auth/register",
		`{"email":"closed@example.com","password":"password123"}`, "")
	if closed.Code != http.StatusForbidden {
		t.Errorf("closed register status = %d, want 403", closed.Code)
	}
	if body := closed.Body.String(); !strings.Contains(body, `"registration is closed"`) {
		t.Errorf("closed register body = %s, want the generic closed message", body)
	}
}

func TestAuthConfig(t *testing.T) {
	s := newTestAuthServer(t)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/auth/config", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var config authConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatalf("config body: %v", err)
	}
	if config.RegistrationOpen {
		t.Error("registrationOpen = true without a store, want false")
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

	malformed := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout", ``, "")
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed logout status = %d, want 400", malformed.Code)
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

func TestAuthRefreshRateLimit(t *testing.T) {
	s := newTestAuthServer(t)

	old := s.refreshLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.refreshLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	body := `{"refresh_token":"refresh-token"}`
	got := []int{
		doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh", body, "").Code,
		doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh", body, "").Code,
		doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh", body, "").Code,
	}
	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d status = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}

	if rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout", body, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("logout after exhaustion status = %d, want 429", rec.Code)
	}
}

func TestAuthLogoutPersistenceFailure(t *testing.T) {
	s := newTestAuthServer(t)
	s.auth = &fakeAuthService{user: newFakeAuthService().user, logoutErr: errors.New("database down")}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout",
		`{"refresh_token":"refresh-token"}`, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if flow := setCookieValue(rec, auth.FlowCookieName); flow == nil || flow.MaxAge >= 0 {
		t.Fatalf("logout 500 did not clear the flow cookie: %+v", flow)
	}
}

func TestAuthLogoutRejectsMalformedBody(t *testing.T) {
	s := newTestAuthServer(t)

	malformed := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout", `{`, "")
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed logout status = %d, want 400", malformed.Code)
	}
	if flow := setCookieValue(malformed, auth.FlowCookieName); flow == nil || flow.MaxAge >= 0 {
		t.Fatalf("logout 400 did not clear the flow cookie: %+v", flow)
	}

	unknown := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout",
		`{"refresh_token":"never-seen"}`, "")
	if unknown.Code != http.StatusNoContent {
		t.Fatalf("unknown-token logout status = %d, want 204", unknown.Code)
	}
}

func TestAuthLogoutRejectsTrailingJSON(t *testing.T) {
	s := newTestAuthServer(t)
	s.auth = &fakeAuthService{user: newFakeAuthService().user, logoutErr: errors.New("logout must not be called")}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout",
		`{"refresh_token":"live-token"} {`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing-JSON logout status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
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

	csp := rec.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"base-uri 'self'", "object-src 'none'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("Content-Security-Policy %q missing %q", csp, directive)
		}
	}
}

func TestContentSecurityPolicyAvatarAllowlist(t *testing.T) {
	s := newTestAuthServer(t)

	rec := doRequest(t, s, http.MethodGet, "/healthz", "", "")
	got := rec.Header().Get("Content-Security-Policy")
	want := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://avatars.githubusercontent.com; base-uri 'self'; object-src 'none'; form-action 'self' https://github.com; frame-ancestors 'none'"
	if got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}

	imgSrc := got[strings.Index(got, "img-src ")+len("img-src "):]
	imgSrc = imgSrc[:strings.Index(imgSrc, ";")]
	var hosts []string
	for _, token := range strings.Fields(imgSrc) {
		if token == "'self'" || token == "data:" || token == "blob:" {
			continue
		}
		hosts = append(hosts, token)
	}
	if len(hosts) != 1 || hosts[0] != "https://avatars.githubusercontent.com" {
		t.Errorf("img-src remote hosts = %q, want [https://avatars.githubusercontent.com]", hosts)
	}
	if !strings.Contains(got, "img-src 'self' data: blob: ") {
		t.Errorf("Content-Security-Policy %q missing the viewer blob: source", got)
	}

	if strings.Contains(got, "*") {
		t.Errorf("Content-Security-Policy %q contains a wildcard source", got)
	}
	for _, scheme := range []string{"http:", "filesystem:"} {
		if strings.Contains(got, scheme) {
			t.Errorf("Content-Security-Policy %q contains loosened scheme %q", got, scheme)
		}
	}
	for _, directive := range []string{"script-src 'self'", "style-src 'self' 'unsafe-inline'", "default-src 'self'"} {
		if !strings.Contains(got, directive) {
			t.Errorf("Content-Security-Policy %q missing pinned %q", got, directive)
		}
	}
}

func TestSecurityHeadersHSTSAndCache(t *testing.T) {
	s := newTestAuthServer(t)

	plain := doRequest(t, s, http.MethodGet, "/healthz", "", "")
	if got := plain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS over plain HTTP = %q, want empty", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got != hstsHeader {
		t.Errorf("HSTS over TLS = %q, want %q", got, hstsHeader)
	}

	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/oauth/github/callback", "/api/v1/tokens"} {
		rec := doRequest(t, s, http.MethodPost, path, "", "")
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control on %s = %q, want no-store", path, got)
		}
	}
	if got := plain.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control on /healthz = %q, want empty", got)
	}
}

func TestTrustedProxyRateLimitKeying(t *testing.T) {
	cfg := &config.Config{
		Values: config.Values{Server: config.Server{
			Addr:           "127.0.0.1",
			Port:           0,
			TrustedProxies: []string{"127.0.0.1"},
		}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, newFakeAuthService(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 1)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	post := func(client string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"email":"user@example.com","password":"password123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}

	if got := post("203.0.113.1"); got != http.StatusOK {
		t.Fatalf("first client status = %d, want 200", got)
	}
	if got := post("203.0.113.2"); got != http.StatusOK {
		t.Fatalf("second client status = %d, want 200 (separate bucket)", got)
	}
	if got := post("203.0.113.1"); got != http.StatusTooManyRequests {
		t.Fatalf("repeat client status = %d, want 429", got)
	}
}
