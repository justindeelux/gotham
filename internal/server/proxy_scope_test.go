package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
)

// unsetPlatformAdmins clears the env var even when another test set it.
func unsetPlatformAdmins(t *testing.T) error {
	t.Helper()
	previous, had := os.LookupEnv(PlatformAdminsEnv)
	if err := os.Unsetenv(PlatformAdminsEnv); err != nil {
		return err
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(PlatformAdminsEnv, previous)
			return
		}
		_ = os.Unsetenv(PlatformAdminsEnv)
	})
	return nil
}

// TestPlatformAdminBoundary proves the platform-global proxy surface (node-wide
// sync, DNS-provider CRUD) is guarded by a real operator boundary instead of
// "any JWT": an admin-scoped API token passes, an operator-listed session email
// passes, and a plain session or a read/deploy token is refused without
// reaching the service.
func TestPlatformAdminBoundary(t *testing.T) {
	s, tokens := newTestTokenServer(t)

	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	// The exact chain internal/server mounts for POST /v1/proxy/sync.
	handler := s.RequireAuth(s.RequirePlatformAdmin(next))

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

	// A plain session (role "user") is refused by default: secure by default.
	if recorder := do("Bearer valid-token"); recorder.Code != http.StatusForbidden {
		t.Fatalf("plain session status = %d, want 403", recorder.Code)
	}
	if calls != 0 {
		t.Fatalf("service called %d times for a plain session", calls)
	}

	// An operator-listed session email passes.
	t.Setenv(PlatformAdminsEnv, "someone@example.com, User@Example.com ")
	if recorder := do("Bearer valid-token"); recorder.Code != http.StatusOK {
		t.Fatalf("listed session status = %d, want 200", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the listed session", calls)
	}

	// An admin-scoped API token passes regardless of the list.
	if err := unsetPlatformAdmins(t); err != nil {
		t.Fatalf("unset %s: %v", PlatformAdminsEnv, err)
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

// TestPlatformAdminRoleSessionPasses proves the JUS-21 session path: a
// session whose access token carries the "admin" role claim (the first
// account's sessions) passes RequirePlatformAdmin with PLATFORM_ADMINS unset,
// while a plain "user" session is still refused.
func TestPlatformAdminRoleSessionPasses(t *testing.T) {
	s, _ := newTestTokenServer(t)

	if err := unsetPlatformAdmins(t); err != nil {
		t.Fatalf("unset %s: %v", PlatformAdminsEnv, err)
	}

	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	// The exact chain internal/server mounts for POST /v1/proxy/sync.
	handler := s.RequireAuth(s.RequirePlatformAdmin(next))

	do := func(authorization string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/sync", nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	if recorder := do("Bearer admin-token"); recorder.Code != http.StatusOK {
		t.Fatalf("admin-role session status = %d, want 200", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the admin-role session", calls)
	}

	if recorder := do("Bearer valid-token"); recorder.Code != http.StatusForbidden {
		t.Fatalf("plain session status = %d, want 403", recorder.Code)
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1 after the refused plain session", calls)
	}
}
