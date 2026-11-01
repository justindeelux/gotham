package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

// TestStorageRoutesRequireAdminScope pins the operator gate on the standalone
// storage API.
func TestStorageRoutesRequireAdminScope(t *testing.T) {
	s, _, owner := scratchProjectsStack(t)
	ownerID, err := uuid.Parse(owner.User.ID)
	if err != nil {
		t.Fatalf("parse owner id: %v", err)
	}

	for _, scope := range []string{auth.ScopeRead, auth.ScopeDeploy} {
		created, err := s.tokens.Create(context.Background(), ownerID, scope, []string{scope})
		if err != nil {
			t.Fatalf("create owner %s token: %v", scope, err)
		}
		for _, route := range []struct {
			name   string
			method string
			path   string
		}{
			{"list storages", http.MethodGet, "/api/v1/storages"},
			{"create storage", http.MethodPost, "/api/v1/storages"},
			{"delete storage", http.MethodDelete, "/api/v1/storages/" + uuid.NewString()},
		} {
			rec := doRequest(t, s, route.method, route.path, "", "Bearer "+created.Token)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("owner %s token %s = %d, want 403 (body %s)", scope, route.name, rec.Code, rec.Body.String())
			}
		}
	}

	admin, err := s.tokens.Create(context.Background(), ownerID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create owner admin token: %v", err)
	}
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/storages", "", "Bearer "+admin.Token); rec.Code != http.StatusOK {
		t.Fatalf("owner admin token storages = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/storages", "", "Bearer "+owner.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("owner session storages = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}
