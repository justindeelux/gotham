package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
)

func TestOwnerBoundary(t *testing.T) {
	s, tokens := newTestTokenServer(t)

	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	handler := s.RequireAuth(s.RequireOwner(next))

	do := func(authorization string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/sync", nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	if recorder := do(""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", recorder.Code)
	}
	if calls != 0 {
		t.Fatalf("service called %d times without a token", calls)
	}

	for _, scope := range []string{auth.ScopeRead, auth.ScopeDeploy} {
		token, err := tokens.Create(context.Background(), testUserID, "scoped", []string{scope})
		if err != nil {
			t.Fatalf("create %s token: %v", scope, err)
		}
		if recorder := do("Bearer " + token.Token); recorder.Code != http.StatusForbidden {
			t.Fatalf("%s token status = %d, want 403", scope, recorder.Code)
		}
		if calls != 0 {
			t.Fatalf("service called %d times for a %s-scoped token", calls, scope)
		}
	}

	if recorder := do("Bearer valid-token"); recorder.Code != http.StatusForbidden {
		t.Fatalf("member session status = %d, want 403", recorder.Code)
	}
	if calls != 0 {
		t.Fatalf("service called %d times for a member session", calls)
	}

	if recorder := do("Bearer owner-token"); recorder.Code != http.StatusOK {
		t.Fatalf("owner session status = %d, want 200", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the owner session", calls)
	}

	adminToken, err := tokens.Create(context.Background(), testUserID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	if recorder := do("Bearer " + adminToken.Token); recorder.Code != http.StatusOK {
		t.Fatalf("admin token status = %d, want 200", recorder.Code)
	}
	if calls != 2 {
		t.Fatalf("service calls = %d, want 2 after the admin token", calls)
	}
}

func TestOwnerRoleSessionPasses(t *testing.T) {
	s, _ := newTestTokenServer(t)

	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	handler := s.RequireAuth(s.RequireOwner(next))

	do := func(authorization string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/sync", nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	if recorder := do("Bearer owner-token"); recorder.Code != http.StatusOK {
		t.Fatalf("owner-role session status = %d, want 200", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the owner-role session", calls)
	}

	if recorder := do("Bearer valid-token"); recorder.Code != http.StatusForbidden {
		t.Fatalf("member session status = %d, want 403", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the refused member session", calls)
	}
}
