package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/backups"
)

var storageOperatorRoutes = []struct {
	name   string
	method string
	path   string
}{
	{"list", http.MethodGet, "/api/v1/storages"},
	{"create", http.MethodPost, "/api/v1/storages"},
	{"update", http.MethodPatch, "/api/v1/storages/44444444-4444-4444-4444-444444444444"},
	{"delete", http.MethodDelete, "/api/v1/storages/44444444-4444-4444-4444-444444444444"},
	{"test", http.MethodPost, "/api/v1/storages/44444444-4444-4444-4444-444444444444/test"},
}

var providerAliasRoutes = []struct {
	name   string
	method string
	path   string
}{
	{"list", http.MethodGet, "/api/v1/backups/providers"},
	{"create", http.MethodPost, "/api/v1/backups/providers"},
	{"update", http.MethodPatch, "/api/v1/backups/providers/44444444-4444-4444-4444-444444444444"},
	{"delete", http.MethodDelete, "/api/v1/backups/providers/44444444-4444-4444-4444-444444444444"},
	{"test", http.MethodPost, "/api/v1/backups/providers/44444444-4444-4444-4444-444444444444/test"},
}

// TestStorageRoutesRequireAdminScope pins the operator gate on the standalone
// storage API, per method.
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
		for _, route := range storageOperatorRoutes {
			rec := doRequest(t, s, route.method, route.path, "{}", "Bearer "+created.Token)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("owner %s token %s = %d, want 403 (body %s)", scope, route.name, rec.Code, rec.Body.String())
			}
		}
	}

	admin, err := s.tokens.Create(context.Background(), ownerID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create owner admin token: %v", err)
	}
	for _, route := range storageOperatorRoutes[:2] {
		rec := doRequest(t, s, route.method, route.path, "{}", "Bearer "+admin.Token)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Fatalf("owner admin token %s = %d (body %s)", route.name, rec.Code, rec.Body.String())
		}
	}
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/storages", "", "Bearer "+owner.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("owner session storages = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestDeprecatedProviderAliasRequiresAdminScope proves the compatibility alias
// cannot be used with a read or deploy token to reach the same service that
// /storages gates behind admin.
func TestDeprecatedProviderAliasRequiresAdminScope(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		backups.Mount(api, s.resourceAuth(), s.platformOnlyAuth, UserIDFromContext, &stubProviderService{})
	})

	for _, scope := range []string{auth.ScopeRead, auth.ScopeDeploy} {
		token := "Bearer " + mustCreateOwnerToken(t, tokens, scope)
		for _, route := range providerAliasRoutes {
			rec := serveHandler(t, router, route.method, route.path, "{}", token)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("owner %s token %s = %d, want 403 (body %s)", scope, route.name, rec.Code, rec.Body.String())
			}
		}
	}

	admin := "Bearer " + mustCreateOwnerToken(t, tokens, auth.ScopeAdmin)
	for _, route := range providerAliasRoutes[:2] {
		rec := serveHandler(t, router, route.method, route.path, "{}", admin)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Fatalf("owner admin token %s = %d (body %s)", route.name, rec.Code, rec.Body.String())
		}
	}
}

type stubProviderService struct {
	backups.BackupService
}

func (s *stubProviderService) ListProviders(context.Context, uuid.UUID) ([]backups.BackupTarget, error) {
	return []backups.BackupTarget{}, nil
}

func (s *stubProviderService) CreateProvider(_ context.Context, _ uuid.UUID, _ backups.ProviderRequest) (backups.BackupTarget, error) {
	return backups.BackupTarget{ID: uuid.New(), Name: "stub", Kind: backups.TargetLocal}, nil
}

func (s *stubProviderService) UpdateProvider(_ context.Context, _ uuid.UUID, id uuid.UUID, _ backups.ProviderRequest) (backups.BackupTarget, error) {
	return backups.BackupTarget{ID: id, Name: "stub", Kind: backups.TargetLocal}, nil
}

func (s *stubProviderService) DeleteProvider(context.Context, uuid.UUID, uuid.UUID, bool) error {
	return nil
}

func (s *stubProviderService) TestProvider(context.Context, uuid.UUID, uuid.UUID) (backups.TargetCheck, error) {
	return backups.TargetCheck{OK: true, Message: "ok"}, nil
}

func serveHandler(t *testing.T, handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
