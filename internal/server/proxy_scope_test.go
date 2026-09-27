package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
)

// TestProxySyncRequiresAdminScope proves the global proxy mutation is guarded
// by the real authentication and scope middleware chain: a missing token gets
// 401, a read/deploy API token gets 403 without reaching the service, and a
// session token (which holds every scope in Phase 1) passes.
func TestProxySyncRequiresAdminScope(t *testing.T) {
	s, tokens := newTestTokenServer(t)

	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	// The exact chain internal/server mounts for POST /v1/proxy/sync.
	handler := s.RequireAuth(RequireScopes(auth.ScopeAdmin)(next))

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

	readToken, err := tokens.Create(context.Background(), testUserID, "read-only", []string{auth.ScopeRead})
	if err != nil {
		t.Fatalf("create read token: %v", err)
	}
	if recorder := do("Bearer " + readToken.Token); recorder.Code != http.StatusForbidden {
		t.Fatalf("read token status = %d, want 403", recorder.Code)
	}
	if calls != 0 {
		t.Fatalf("service called %d times for a read-scoped token", calls)
	}

	deployToken, err := tokens.Create(context.Background(), testUserID, "deploy-only", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatalf("create deploy token: %v", err)
	}
	if recorder := do("Bearer " + deployToken.Token); recorder.Code != http.StatusForbidden {
		t.Fatalf("deploy token status = %d, want 403", recorder.Code)
	}
	if calls != 0 {
		t.Fatalf("service called %d times for a deploy-scoped token", calls)
	}

	adminToken, err := tokens.Create(context.Background(), testUserID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	if recorder := do("Bearer " + adminToken.Token); recorder.Code != http.StatusOK {
		t.Fatalf("admin token status = %d, want 200", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 for the admin token", calls)
	}

	// A session token carries every scope in Phase 1.
	if recorder := do("Bearer valid-token"); recorder.Code != http.StatusOK {
		t.Fatalf("session token status = %d, want 200", recorder.Code)
	}
	if calls != 2 {
		t.Fatalf("service calls = %d, want 2 after the session token", calls)
	}
}
