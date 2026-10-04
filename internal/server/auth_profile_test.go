package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/auth"
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

var _ = auth.ErrDisplayNameInvalid
