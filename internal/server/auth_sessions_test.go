package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

// TestSessionsList: the list returns the fake's rows with the contract shape;
// the sid-bound token marks its row current, a sid-less token marks none.
func TestSessionsList(t *testing.T) {
	s, _ := newTestProfileServer(t)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer sid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body sessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("list returned %d rows, want 2", len(body.Sessions))
	}
	first := body.Sessions[0]
	if first.ID != fakeSessions[0].ID || !first.Current {
		t.Errorf("first row = %+v, want the sid-bound session marked current", first)
	}
	if first.UserAgent != "test-agent" || first.IP != "10.0.0.1" {
		t.Errorf("first row meta = %q/%q, want test-agent/10.0.0.1", first.UserAgent, first.IP)
	}
	if body.Sessions[1].Current {
		t.Errorf("second row = %+v, want current=false", body.Sessions[1])
	}
	for _, want := range []string{`"user_agent"`, `"ip"`, `"created_at"`, `"last_used_at"`, `"current"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("list body %s missing %s", rec.Body.String(), want)
		}
	}

	sidLess := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer valid-token")
	if sidLess.Code != http.StatusOK {
		t.Fatalf("sid-less list status = %d, want 200", sidLess.Code)
	}
	var sidLessBody sessionsResponse
	if err := json.Unmarshal(sidLess.Body.Bytes(), &sidLessBody); err != nil {
		t.Fatalf("decode sid-less body: %v", err)
	}
	for _, session := range sidLessBody.Sessions {
		if session.Current {
			t.Errorf("sid-less token marked %v current, want none", session.ID)
		}
	}

	missing := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "")
	if missing.Code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", missing.Code)
	}
}

func TestSessionsRevoke(t *testing.T) {
	s, _ := newTestProfileServer(t)

	ok := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+fakeSessions[1].ID.String(), "", "Bearer sid-token")
	if ok.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204 (body %s)", ok.Code, ok.Body.String())
	}

	unknown := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/44444444-2222-3333-4444-555555555555", "", "Bearer sid-token")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404", unknown.Code)
	}
	if body := unknown.Body.String(); !strings.Contains(body, `"session not found"`) {
		t.Errorf("unknown id body = %s, want the contract message", body)
	}

	malformed := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/not-a-uuid", "", "Bearer sid-token")
	if malformed.Code != http.StatusNotFound {
		t.Fatalf("malformed id status = %d, want 404", malformed.Code)
	}
	if body := malformed.Body.String(); !strings.Contains(body, `"session not found"`) {
		t.Errorf("malformed id body = %s, want the contract message", body)
	}

	missing := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+fakeSessions[1].ID.String(), "", "")
	if missing.Code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", missing.Code)
	}
}

func TestSessionsRevokeOthers(t *testing.T) {
	s, _ := newTestProfileServer(t)

	ok := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", "Bearer sid-token")
	if ok.Code != http.StatusNoContent {
		t.Fatalf("revoke-others status = %d, want 204 (body %s)", ok.Code, ok.Body.String())
	}

	// A token without a sid claim cannot name the current session: 409.
	sidLess := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", "Bearer valid-token")
	if sidLess.Code != http.StatusConflict {
		t.Fatalf("sid-less revoke-others status = %d, want 409 (body %s)", sidLess.Code, sidLess.Body.String())
	}
	if body := sidLess.Body.String(); !strings.Contains(body, `"sign in again to manage other sessions"`) {
		t.Errorf("sid-less body = %s, want the contract message", body)
	}

	missing := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", "")
	if missing.Code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", missing.Code)
	}
}

// TestSessionsRejectsAPITokens: a scoped API token must never list or revoke
// sessions: 403 with the contract message on all three routes.
func TestSessionsRejectsAPITokens(t *testing.T) {
	s, tokens := newTestProfileServer(t)

	created, err := tokens.Create(context.Background(), testUserID, "ci", []string{"admin"})
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	bearer := "Bearer " + created.Token

	list := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", bearer)
	if list.Code != http.StatusForbidden {
		t.Fatalf("API-token list status = %d, want 403 (body %s)", list.Code, list.Body.String())
	}

	revoke := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+fakeSessions[0].ID.String(), "", bearer)
	if revoke.Code != http.StatusForbidden {
		t.Fatalf("API-token revoke status = %d, want 403 (body %s)", revoke.Code, revoke.Body.String())
	}

	others := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", bearer)
	if others.Code != http.StatusForbidden {
		t.Fatalf("API-token revoke-others status = %d, want 403 (body %s)", others.Code, others.Body.String())
	}

	for name, rec := range map[string]string{"list": list.Body.String(), "revoke": revoke.Body.String(), "others": others.Body.String()} {
		if !strings.Contains(rec, `"this action needs an interactive session"`) {
			t.Errorf("API-token %s body = %s, want the contract message", name, rec)
		}
	}
}

// TestSessionsRateLimit: the session routes sit behind the credential limiter
// with the profile group.
func TestSessionsRateLimit(t *testing.T) {
	s, _ := newTestProfileServer(t)

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(0), 2)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	first := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer sid-token")
	second := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer sid-token")
	third := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer sid-token")
	if first.Code != http.StatusOK || second.Code != http.StatusOK || third.Code != http.StatusTooManyRequests {
		t.Fatalf("list statuses = %d,%d,%d, want 200,200,429", first.Code, second.Code, third.Code)
	}

	revoke := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+fakeSessions[0].ID.String(), "", "Bearer sid-token")
	if revoke.Code != http.StatusTooManyRequests {
		t.Fatalf("revoke after exhaustion status = %d, want 429", revoke.Code)
	}

	others := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", "Bearer sid-token")
	if others.Code != http.StatusTooManyRequests {
		t.Fatalf("revoke-others after exhaustion status = %d, want 429", others.Code)
	}
}

// TestSessionsEndToEndOverHTTP replays the contract against the real stack:
// list with the current marker, revoke-other, revoke-own, foreign 404,
// revoke-others, and the empty-list shape.
func TestSessionsEndToEndOverHTTP(t *testing.T) {
	s, _, pair := scratchProfileStack(t)
	bearer := "Bearer " + pair.AccessToken

	// The stack seeds its sessions through direct service calls (zero device
	// metadata), so lift the credential limiter for the transcript below.
	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(1000), 1000)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	list := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", bearer)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", list.Code, list.Body.String())
	}
	var body sessionsResponse
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("list returned %d rows, want the register and login sessions", len(body.Sessions))
	}
	current := 0
	for _, session := range body.Sessions {
		if session.Current {
			current++
		}
		if session.UserAgent != "" || session.IP != "" {
			t.Errorf("row %+v: want empty metadata (the stack seeds zero meta)", session)
		}
	}
	if current != 1 {
		t.Errorf("list marks %d rows current, want exactly 1", current)
	}
	var otherID, ownID string
	for _, session := range body.Sessions {
		if session.Current {
			ownID = session.ID.String()
		} else {
			otherID = session.ID.String()
		}
	}

	// Revoke the other session: 204, and it leaves the list.
	if rec := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+otherID, "", bearer); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke other status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	again := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", bearer)
	var remaining sessionsResponse
	if err := json.Unmarshal(again.Body.Bytes(), &remaining); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(remaining.Sessions) != 1 || !remaining.Sessions[0].Current {
		t.Fatalf("list after revoke = %+v, want only the current session", remaining.Sessions)
	}

	// A foreign id is 404, never 403.
	foreign := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/44444444-2222-3333-4444-555555555555", "", bearer)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign id status = %d, want 404", foreign.Code)
	}
	if body := foreign.Body.String(); !strings.Contains(body, `"session not found"`) {
		t.Errorf("foreign id body = %s, want the contract message", body)
	}

	// Revoke-others with a single live session is a 204 no-op.
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", bearer); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke-others status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	// Ending the caller's own session is allowed; the list is then empty
	// (an array, never null).
	if rec := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+ownID, "", bearer); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke own status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	empty := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", bearer)
	if empty.Code != http.StatusOK {
		t.Fatalf("empty list status = %d, want 200", empty.Code)
	}
	if !strings.Contains(empty.Body.String(), `"sessions":[]`) {
		t.Errorf("empty list body = %s, want an empty array", empty.Body.String())
	}
}

// TestSessionsCaptureMetaOverHTTP: a refresh over HTTP records the request's
// User-Agent and client IP on the rotated session, and the list shows them.
func TestSessionsCaptureMetaOverHTTP(t *testing.T) {
	s, _, pair := scratchProfileStack(t)

	refreshBody := `{"refresh_token":"` + pair.RefreshToken + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(refreshBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pf2-test-agent/1.0")
	req.RemoteAddr = "203.0.113.7:1234"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var refreshed authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}

	list := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer "+refreshed.AccessToken)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", list.Code, list.Body.String())
	}
	var body sessionsResponse
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	for _, session := range body.Sessions {
		if !session.Current {
			continue
		}
		if session.UserAgent != "pf2-test-agent/1.0" {
			t.Errorf("current UA = %q, want the refresh request value", session.UserAgent)
		}
		if session.IP != "203.0.113.7" {
			t.Errorf("current IP = %q, want the refresh peer", session.IP)
		}
		return
	}
	t.Error("no session marked current after the HTTP refresh")
}

// TestSessionsRevokeReplayStaysPlain401OverHTTP (F1): revoking a session
// deletes its row, so replaying its refresh token is a plain 401 that leaves
// every other session alone — including over HTTP.
func TestSessionsRevokeReplayStaysPlain401OverHTTP(t *testing.T) {
	s, reg, login := scratchProfileStack(t)

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(1000), 1000)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	bearer := "Bearer " + login.AccessToken
	var body sessionsResponse
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", bearer); rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	} else if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("list returned %d rows, want 2", len(body.Sessions))
	}
	var otherID string
	for _, session := range body.Sessions {
		if !session.Current {
			otherID = session.ID.String()
		}
	}

	// Revoke the other session, then replay its refresh token: 401, and the
	// current session is untouched.
	if rec := doRequest(t, s, http.MethodDelete, "/api/v1/auth/me/sessions/"+otherID, "", bearer); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	replay := doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh",
		`{"refresh_token":"`+reg.RefreshToken+`"}`, "")
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, want 401 (body %s)", replay.Code, replay.Body.String())
	}
	again := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", bearer)
	var remaining sessionsResponse
	if err := json.Unmarshal(again.Body.Bytes(), &remaining); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(remaining.Sessions) != 1 || !remaining.Sessions[0].Current {
		t.Fatalf("list after replay = %+v, want only the current session", remaining.Sessions)
	}
	refreshed := doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh",
		`{"refresh_token":"`+login.RefreshToken+`"}`, "")
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh after replay status = %d, want 200 (the family must be intact)", refreshed.Code)
	}
}

// TestSessionsStaleSidConflictOverHTTP (F2): revoke-others with the
// pre-rotation access token answers 409 and deletes nothing.
func TestSessionsStaleSidConflictOverHTTP(t *testing.T) {
	s, _, login := scratchProfileStack(t)

	old := s.authLimiter
	limiter := newIPRateLimiter(rate.Limit(1000), 1000)
	s.authLimiter = limiter
	t.Cleanup(func() {
		limiter.Close()
		old.Close()
	})

	rotated := doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh",
		`{"refresh_token":"`+login.RefreshToken+`"}`, "")
	if rotated.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", rotated.Code)
	}
	var fresh authResponse
	if err := json.Unmarshal(rotated.Body.Bytes(), &fresh); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}

	stale := doRequest(t, s, http.MethodPost, "/api/v1/auth/me/sessions/revoke-others", "", "Bearer "+login.AccessToken)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale-sid revoke-others status = %d, want 409 (body %s)", stale.Code, stale.Body.String())
	}
	if body := stale.Body.String(); !strings.Contains(body, `"sign in again to manage other sessions"`) {
		t.Errorf("stale-sid body = %s, want the contract message", body)
	}

	// Nothing was revoked: both sessions still list under the fresh token.
	list := doRequest(t, s, http.MethodGet, "/api/v1/auth/me/sessions", "", "Bearer "+fresh.AccessToken)
	var body sessionsResponse
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Errorf("list after refused revoke-others = %d rows, want 2", len(body.Sessions))
	}
}

// TestProfileFormatCharsOverHTTP: the F5 fix at the HTTP layer — a ZWNJ name
// and a ZWJ emoji round-trip with 200, a word-joiner name is a 400.
func TestProfileFormatCharsOverHTTP(t *testing.T) {
	s, _, pair := scratchProfileStack(t)
	bearer := "Bearer " + pair.AccessToken

	// A Persian name joined with ZWNJ plus a ZWJ emoji sequence: 200.
	allowed, _ := json.Marshal(map[string]string{"display_name": "\u0639\u0644\u06cc\u200c\u0631\u0636\u0627 \U0001F468\u200d\U0001F469\u200d\U0001F467"})
	ok := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", string(allowed), bearer)
	if ok.Code != http.StatusOK {
		t.Fatalf("ZWNJ/ZWJ name status = %d, want 200 (body %s)", ok.Code, ok.Body.String())
	}

	// A word-joiner name: 400 with the contract message.
	denied, _ := json.Marshal(map[string]string{"display_name": "a\u2060b"})
	deniedRec := doRequest(t, s, http.MethodPatch, "/api/v1/auth/me", string(denied), bearer)
	if deniedRec.Code != http.StatusBadRequest {
		t.Fatalf("word-joiner name status = %d, want 400 (body %s)", deniedRec.Code, deniedRec.Body.String())
	}
	if body := deniedRec.Body.String(); !strings.Contains(body, `"display name must be 1-64 characters"`) {
		t.Errorf("word-joiner body = %s, want the contract message", body)
	}
}
