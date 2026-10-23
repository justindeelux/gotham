package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

func decodeAuthBody(t *testing.T, raw []byte) authResponse {
	t.Helper()
	var body authResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode auth body: %v", err)
	}
	return body
}

func TestLoginSecondFactorChallenge(t *testing.T) {
	s := newTestAuthServer(t)
	s.auth.(*fakeAuthService).mfaLogin = true

	login := doRequest(t, s, http.MethodPost, "/api/v1/auth/login",
		`{"email":"user@example.com","password":"password123"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", login.Code, login.Body.String())
	}
	challenge := decodeAuthBody(t, login.Body.Bytes())
	if !challenge.MFARequired || challenge.MFAToken != "mfa-token" {
		t.Fatalf("login body = %+v, want mfa_required with a token", challenge)
	}
	if challenge.AccessToken != "" || challenge.RefreshToken != "" {
		t.Fatalf("the half-authenticated step issued tokens: %+v", challenge)
	}

	complete := doRequest(t, s, http.MethodPost, "/api/v1/auth/login/totp",
		`{"mfa_token":"mfa-token","code":"123456"}`, "")
	if complete.Code != http.StatusOK {
		t.Fatalf("login/totp status = %d, want 200 (body %s)", complete.Code, complete.Body.String())
	}
	if body := decodeAuthBody(t, complete.Body.Bytes()); body.AccessToken == "" || body.RefreshToken == "" {
		t.Fatalf("login/totp body = %+v, want a token pair", body)
	}

	for name, body := range map[string]string{
		"wrong code": `{"mfa_token":"mfa-token","code":"000000"}`,
		"bad token":  `{"mfa_token":"nope","code":"123456"}`,
		"empty":      `{}`,
	} {
		rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/login/totp", body, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401 (body %s)", name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"invalid code"`) {
			t.Errorf("%s body = %s, want the generic message", name, rec.Body.String())
		}
	}
}

func TestTOTPLockoutMapsTo429(t *testing.T) {
	s := newTestAuthServer(t)
	unlimitAuthLimiter(t, s)

	login := doRequest(t, s, http.MethodPost, "/api/v1/auth/login/totp",
		`{"mfa_token":"mfa-token","code":"locked"}`, "")
	if login.Code != http.StatusTooManyRequests {
		t.Fatalf("locked login/totp = %d %s, want 429", login.Code, login.Body.String())
	}
	if !strings.Contains(login.Body.String(), "too many incorrect codes") {
		t.Fatalf("locked login/totp body = %s, want the lockout message", login.Body.String())
	}

	disable := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/disable",
		`{"password":"password123","code":"locked"}`, "Bearer valid-token")
	if disable.Code != http.StatusTooManyRequests {
		t.Fatalf("locked disable = %d %s, want 429", disable.Code, disable.Body.String())
	}
}

func TestLoginSecondFactorRateLimit(t *testing.T) {
	s := newTestAuthServer(t)

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	body := `{"mfa_token":"mfa-token","code":"123456"}`
	got := []int{
		doRequest(t, s, http.MethodPost, "/api/v1/auth/login/totp", body, "").Code,
		doRequest(t, s, http.MethodPost, "/api/v1/auth/login/totp", body, "").Code,
		doRequest(t, s, http.MethodPost, "/api/v1/auth/login/totp", body, "").Code,
	}
	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d status = %d, want %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

func TestTOTPProfileRoutes(t *testing.T) {
	s := newTestAuthServer(t)
	unlimitAuthLimiter(t, s)

	status := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/totp", "", "Bearer valid-token")
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", status.Code, status.Body.String())
	}
	var state struct {
		Enabled                bool `json:"enabled"`
		RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if state.Enabled {
		t.Fatalf("status = %+v, want disabled by default", state)
	}

	enroll := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/enroll", `{}`, "Bearer valid-token")
	if enroll.Code != http.StatusOK {
		t.Fatalf("enroll status = %d, want 200 (body %s)", enroll.Code, enroll.Body.String())
	}
	if !strings.Contains(enroll.Body.String(), `"otpauth_url"`) {
		t.Fatalf("enroll body = %s, want an otpauth URL", enroll.Body.String())
	}

	enable := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/enable",
		`{"password":"password123","code":"123456"}`, "Bearer valid-token")
	if enable.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want 200 (body %s)", enable.Code, enable.Body.String())
	}
	if !strings.Contains(enable.Body.String(), "ABCDE-FGHJK") {
		t.Fatalf("enable body = %s, want the recovery codes", enable.Body.String())
	}

	wrongCode := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/enable",
		`{"password":"password123","code":"000000"}`, "Bearer valid-token")
	if wrongCode.Code != http.StatusBadRequest || !strings.Contains(wrongCode.Body.String(), `"invalid code"`) {
		t.Fatalf("enable with a wrong code = %d %s, want 400 invalid code", wrongCode.Code, wrongCode.Body.String())
	}

	wrongPassword := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/enable",
		`{"password":"wrong-password","code":"123456"}`, "Bearer valid-token")
	if wrongPassword.Code != http.StatusBadRequest || !strings.Contains(wrongPassword.Body.String(), "current password is incorrect") {
		t.Fatalf("enable with a wrong password = %d %s, want 400", wrongPassword.Code, wrongPassword.Body.String())
	}

	regenerate := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/recovery-codes",
		`{"password":"password123","code":"123456"}`, "Bearer valid-token")
	if regenerate.Code != http.StatusOK {
		t.Fatalf("regenerate status = %d, want 200 (body %s)", regenerate.Code, regenerate.Body.String())
	}

	disable := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/totp/disable",
		`{"password":"password123","code":"123456"}`, "Bearer valid-token")
	if disable.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d, want 204 (body %s)", disable.Code, disable.Body.String())
	}
}

func TestTOTPRejectsAPITokens(t *testing.T) {
	s, tokens := newTestProfileServer(t)
	unlimitAuthLimiter(t, s)

	created, err := tokens.Create(context.Background(), testUserID, "ci", []string{"admin"})
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	bearer := "Bearer " + created.Token

	for _, route := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/auth/me/totp", ""},
		{http.MethodPost, "/api/v1/auth/me/totp/enroll", `{}`},
		{http.MethodPost, "/api/v1/auth/me/totp/enable", `{"password":"x","code":"123456"}`},
		{http.MethodPost, "/api/v1/auth/me/totp/disable", `{"password":"x","code":"123456"}`},
		{http.MethodPost, "/api/v1/auth/me/totp/recovery-codes", `{"password":"x","code":"123456"}`},
	} {
		rec := doRequest(t, s, route.method, route.path, route.body, bearer)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with an API token = %d, want 403 (body %s)", route.method, route.path, rec.Code, rec.Body.String())
		}
	}
}

func unlimitAuthLimiter(t *testing.T, s *Server) {
	t.Helper()
	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Inf, 1)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})
}
