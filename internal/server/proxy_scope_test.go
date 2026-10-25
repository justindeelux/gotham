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
		t.Fatalf("create member admin token: %v", err)
	}
	if recorder := do("Bearer " + adminToken.Token); recorder.Code != http.StatusForbidden {
		t.Fatalf("member-owned admin token status = %d, want 403", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the member-owned admin token", calls)
	}

	ownerToken, err := tokens.Create(context.Background(), testOwnerID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create owner admin token: %v", err)
	}
	if recorder := do("Bearer " + ownerToken.Token); recorder.Code != http.StatusOK {
		t.Fatalf("owner-owned admin token status = %d, want 200", recorder.Code)
	}
	if calls != 2 {
		t.Fatalf("service calls = %d, want 2 after the owner-owned admin token", calls)
	}
}

func TestDemotedOwnerLosesOperatorAccess(t *testing.T) {
	s, tokens := newTestTokenServer(t)

	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	handler := s.RequireAuth(s.RequireOwner(next))

	do := func(authorization string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/sync", nil)
		request.Header.Set("Authorization", authorization)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	adminToken, err := tokens.Create(context.Background(), testOwnerID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create owner admin token: %v", err)
	}
	if recorder := do("Bearer owner-token"); recorder.Code != http.StatusOK {
		t.Fatalf("owner session status = %d, want 200", recorder.Code)
	}
	if recorder := do("Bearer " + adminToken.Token); recorder.Code != http.StatusOK {
		t.Fatalf("owner admin token status = %d, want 200", recorder.Code)
	}

	s.auth.(*fakeAuthService).users[testOwnerID].Role = auth.RoleMember

	if recorder := do("Bearer owner-token"); recorder.Code != http.StatusForbidden {
		t.Fatalf("demoted owner session status = %d, want 403", recorder.Code)
	}
	if recorder := do("Bearer " + adminToken.Token); recorder.Code != http.StatusForbidden {
		t.Fatalf("demoted owner admin token status = %d, want 403", recorder.Code)
	}
	if calls != 2 {
		t.Fatalf("service calls = %d, want only the two pre-demotion requests", calls)
	}

	delete(s.auth.(*fakeAuthService).users, testOwnerID)

	if recorder := do("Bearer owner-token"); recorder.Code != http.StatusForbidden {
		t.Fatalf("removed owner session status = %d, want 403", recorder.Code)
	}
	if recorder := do("Bearer " + adminToken.Token); recorder.Code != http.StatusForbidden {
		t.Fatalf("removed owner admin token status = %d, want 403", recorder.Code)
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
