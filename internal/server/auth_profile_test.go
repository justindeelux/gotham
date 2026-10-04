package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"github.com/go-chi/chi/v5"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/store"
)

// newTestProfileServer builds a Server whose RequireAuth also resolves API
// tokens, so the profile routes can prove they reject them.
func newTestProfileServer(t *testing.T) (*Server, *fakeTokenService) {
	t.Helper()

	s, tokens := newTestTokenServer(t)
	return s, tokens
}

func TestProfileUpdate(t *testing.T) {
	s, _ := newTestProfileServer(t)

	updated := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me",
		`{"display_name":"  Ada  "}`, "Bearer valid-token")
	if updated.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200 (body %s)", updated.Code, updated.Body.String())
	}
	var body meResponse
	if err := json.Unmarshal(updated.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode patch body: %v", err)
	}
	if body.User == nil || body.User.DisplayName == nil || *body.User.DisplayName != "  Ada  " {
		t.Errorf("patch body user = %+v, want the fake's stored name", body.User)
	}

	cleared := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me",
		`{"display_name":null}`, "Bearer valid-token")
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200 (body %s)", cleared.Code, cleared.Body.String())
	}

	tooLong := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me",
		`{"display_name":"`+strings.Repeat("x", 65)+`"}`, "Bearer valid-token")
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("long name status = %d, want 400", tooLong.Code)
	}
	if body := tooLong.Body.String(); !strings.Contains(body, `"display name must be 1-64 characters"`) {
		t.Errorf("long name body = %s, want the contract message", body)
	}

	missing := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", "", "")
	if missing.Code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", missing.Code)
	}
}

func TestProfileMeCarriesNewFields(t *testing.T) {
	s, _ := newTestProfileServer(t)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer valid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"has_password"`, `"is_platform_admin"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("me body %s missing %s", rec.Body.String(), want)
		}
	}
}

// TestMePlatformAdminSources: GET /me reports is_platform_admin from the
// stored flag OR a PLATFORM_ADMINS allowlist match, using the same helper as
// the admin gate.
func TestMePlatformAdminSources(t *testing.T) {
	s, _ := newTestProfileServer(t)

	// The fake account has no stored flag and the allowlist is empty.
	t.Setenv(PlatformAdminsEnv, "")
	plain := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer valid-token")
	if plain.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200", plain.Code)
	}
	var plainBody meResponse
	if err := json.Unmarshal(plain.Body.Bytes(), &plainBody); err != nil {
		t.Fatalf("decode me body: %v", err)
	}
	if plainBody.User.IsPlatformAdmin {
		t.Error("is_platform_admin = true, want false with no flag and no allowlist")
	}

	// The allowlist alone flips the bit (case-insensitive, like the gate).
	t.Setenv(PlatformAdminsEnv, "USER@example.com")
	listed := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer valid-token")
	var listedBody meResponse
	if err := json.Unmarshal(listed.Body.Bytes(), &listedBody); err != nil {
		t.Fatalf("decode me body: %v", err)
	}
	if !listedBody.User.IsPlatformAdmin {
		t.Errorf("is_platform_admin = false for an allowlisted email, want true (body %s)", listed.Body.String())
	}

	// The stored flag alone flips the bit: the scratch bootstrap account.
	t.Setenv(PlatformAdminsEnv, "")
	stack, _, pair := scratchProfileStack(t)
	stored := doRequest(t, stack, http.MethodGet, "/api/v1/auth/me", "", "Bearer "+pair.AccessToken)
	var storedBody meResponse
	if err := json.Unmarshal(stored.Body.Bytes(), &storedBody); err != nil {
		t.Fatalf("decode me body: %v", err)
	}
	if !storedBody.User.IsPlatformAdmin {
		t.Errorf("is_platform_admin = false for the bootstrap account, want true (body %s)", stored.Body.String())
	}
}

func TestProfileChangePassword(t *testing.T) {
	s, _ := newTestProfileServer(t)

	ok := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/password",
		`{"current_password":"old-password","new_password":"new-s3cret-password"}`, "Bearer valid-token")
	if ok.Code != http.StatusOK {
		t.Fatalf("change status = %d, want 200 (body %s)", ok.Code, ok.Body.String())
	}
	var changed authResponse
	if err := json.Unmarshal(ok.Body.Bytes(), &changed); err != nil {
		t.Fatalf("decode change body: %v", err)
	}
	if changed.AccessToken == "" || changed.RefreshToken == "" || changed.User == nil {
		t.Errorf("change body missing the fresh pair: %+v", changed)
	}

	wrong := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/password",
		`{"current_password":"wrong-password","new_password":"new-s3cret-password"}`, "Bearer valid-token")
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong current status = %d, want 400 (body %s)", wrong.Code, wrong.Body.String())
	}
	if body := wrong.Body.String(); !strings.Contains(body, `"current password is incorrect"`) {
		t.Errorf("wrong current body = %s, want the contract message", body)
	}

	missing := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/password",
		`{"current_password":"old-password","new_password":"new-s3cret-password"}`, "")
	if missing.Code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", missing.Code)
	}
}

// TestProfileRejectsAPITokens: a scoped API token (even admin-worthy) must
// never touch the self-service profile routes: 403 with the contract message.
func TestProfileRejectsAPITokens(t *testing.T) {
	s, tokens := newTestProfileServer(t)

	created, err := tokens.Create(context.Background(), testUserID, "ci", []string{"admin"})
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	bearer := "Bearer " + created.Token

	patch := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me",
		`{"display_name":"Ada"}`, bearer)
	if patch.Code != http.StatusForbidden {
		t.Fatalf("API-token patch status = %d, want 403 (body %s)", patch.Code, patch.Body.String())
	}
	if body := patch.Body.String(); !strings.Contains(body, `"this action needs an interactive session"`) {
		t.Errorf("API-token patch body = %s, want the contract message", body)
	}

	password := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/password",
		`{"current_password":"old-password","new_password":"new-s3cret-password"}`, bearer)
	if password.Code != http.StatusForbidden {
		t.Fatalf("API-token password status = %d, want 403 (body %s)", password.Code, password.Body.String())
	}
	if body := password.Body.String(); !strings.Contains(body, `"this action needs an interactive session"`) {
		t.Errorf("API-token password body = %s, want the contract message", body)
	}

	// The API token itself is valid: the tokens surface still answers 200.
	listed := doRequest(t, s, http.MethodGet, "/api/v1/tokens", "", bearer)
	if listed.Code != http.StatusOK {
		t.Fatalf("API-token list status = %d, want 200 (token must stay valid)", listed.Code)
	}
}

// TestProfileRateLimit: the profile routes sit behind the credential limiter.
func TestProfileRateLimit(t *testing.T) {
	s, _ := newTestProfileServer(t)

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	body := `{"display_name":"Ada"}`
	got := []int{
		doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", body, "Bearer valid-token").Code,
		doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", body, "Bearer valid-token").Code,
		doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", body, "Bearer valid-token").Code,
	}
	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d status = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}

	password := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/password",
		`{"current_password":"old-password","new_password":"new-s3cret-password"}`, "Bearer valid-token")
	if password.Code != http.StatusTooManyRequests {
		t.Fatalf("password after exhaustion status = %d, want 429", password.Code)
	}
}

// TestRequireInteractiveSessionMiddleware: the guard is route-level
// middleware, so a route mounted in the group is rejected for API tokens
// without any per-handler code. The dummy handler proves the middleware (not
// the handler) wrote the 403.
func TestRequireInteractiveSessionMiddleware(t *testing.T) {
	s, _ := newTestProfileServer(t)

	r := chi.NewRouter()
	r.Use(s.requireInteractiveSession)
	r.Get("/guarded", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, apiError{Message: "reached"})
	})

	apiTokenReq := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	apiTokenReq = apiTokenReq.WithContext(context.WithValue(apiTokenReq.Context(), apiTokenKey, true))
	apiTokenRec := httptest.NewRecorder()
	r.ServeHTTP(apiTokenRec, apiTokenReq)
	if apiTokenRec.Code != http.StatusForbidden {
		t.Fatalf("API-token status = %d, want 403", apiTokenRec.Code)
	}
	if body := apiTokenRec.Body.String(); !strings.Contains(body, `"this action needs an interactive session"`) {
		t.Fatalf("API-token body = %s, want the contract message", body)
	}

	plainReq := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	plainRec := httptest.NewRecorder()
	r.ServeHTTP(plainRec, plainReq)
	if plainRec.Code != http.StatusOK {
		t.Fatalf("interactive status = %d, want 200", plainRec.Code)
	}
}

// scratchProfileStack builds a Server backed by the real auth service on a
// private scratch database and returns it with the service and live token
// pairs for the seeded account (registered and logged in). Fake-backed
// handler tests cannot catch validation the fake does not mirror (F1 reached
// Postgres and answered 500); this stack can.
func scratchProfileStack(t *testing.T) (*Server, *auth.AuthResult, *auth.AuthResult) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	base := os.Getenv("GOTHAM_TEST_DSN")
	if base == "" {
		base = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	name := fmt.Sprintf("pf1srv%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		admin, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()

	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open scratch store: %v", err)
	}
	t.Cleanup(pool.Close)

	signer, err := auth.NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	svc := auth.New(store.New(pool), signer, slog.New(slog.NewTextHandler(io.Discard, nil)))

	email := fmt.Sprintf("pf1-srv-%d@example.com", time.Now().UnixNano())
	registered, err := svc.Register(ctx, email, "s3cret-password", "", nil, auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	loggedIn, err := svc.Login(ctx, email, "s3cret-password", auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, svc, nil, nil, nil, store.New(pool))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s, registered, loggedIn
}

// TestProfileUnstorableTextOverHTTP replays the F1 probe against the real
// stack: NUL, bidi-override and zero-width names are 400 with the contract
// message, and a storable name still round-trips.
func TestProfileUnstorableTextOverHTTP(t *testing.T) {
	s, _, pair := scratchProfileStack(t)
	bearer := "Bearer " + pair.AccessToken

	for name, payload := range map[string]string{
		"NUL":        `{"display_name":"a\u0000b"}`,
		"bidi":       `{"display_name":"a\u202eb"}`,
		"zero-width": `{"display_name":"a\u200bb"}`,
	} {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", payload, bearer)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", name, rec.Code, rec.Body.String())
		} else if body := rec.Body.String(); !strings.Contains(body, `"display name must be 1-64 characters"`) {
			t.Errorf("%s: body = %s, want the contract message", name, body)
		}
	}

	ok := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me",
		`{"display_name":"Ada Lovelace"}`, bearer)
	if ok.Code != http.StatusOK {
		t.Fatalf("valid name status = %d, want 200 (body %s)", ok.Code, ok.Body.String())
	}
	var body meResponse
	if err := json.Unmarshal(ok.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.User == nil || body.User.DisplayName == nil || *body.User.DisplayName != "Ada Lovelace" {
		t.Fatalf("stored name = %+v, want Ada Lovelace", body.User)
	}
}
