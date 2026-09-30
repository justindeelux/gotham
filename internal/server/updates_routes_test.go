package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

// fakeReleasesServer serves a single newer release with the assets the Checker
// resolves for the running architecture.
func fakeReleasesServer(t *testing.T) *httptest.Server {
	t.Helper()
	asset := "gotham-linux-" + runtime.GOARCH
	manifest := "gotham-manifest-" + runtime.GOARCH + ".txt"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		assets := []map[string]any{
			{"name": asset, "browser_download_url": base + "/" + asset, "size": 1024},
			{"name": manifest, "browser_download_url": base + "/" + manifest, "size": 200},
			{"name": manifest + ".sig", "browser_download_url": base + "/" + manifest + ".sig", "size": 64},
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"tag_name":     "v1.2.0",
			"prerelease":   false,
			"draft":        false,
			"body":         "notes",
			"published_at": "2026-01-02T15:04:05Z",
			"assets":       assets,
		}})
	}))
	t.Cleanup(server.Close)
	return server
}

// newUpdateTestServer builds a Server with the self-update service pointed at a
// local release API and a fixed running version.
func newUpdateTestServer(t *testing.T, releasesURL string) (*Server, *fakeTokenService) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FEATURE_UPDATES", "true")
	t.Setenv("GOTHAM_UPDATE_BASE_URL", releasesURL)
	t.Setenv("GOTHAM_UPDATE_CURRENT", "v1.0.0")
	t.Setenv("GOTHAM_UPDATE_PUBLIC_KEY", "")
	t.Setenv("GOTHAM_UPDATE_BINARY", filepath.Join(dir, "gotham"))
	t.Setenv("GOTHAM_UPDATE_LOCK", filepath.Join(dir, "update.lock"))
	t.Setenv("GOTHAM_UPDATE_STATUS", filepath.Join(dir, "status", "update.status"))
	t.Setenv("GOTHAM_UPDATE_PENDING", filepath.Join(dir, "update.pending"))
	t.Setenv("GOTHAM_UPDATE_SCRIPT", filepath.Join(dir, "gotham-update"))

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := newFakeTokenService()

	s, err := New(cfg, logger, newFakeAuthService(), nil, tokens, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s, tokens
}

// TestUpdateRoutesGating proves check requires a session and apply requires a
// platform operator: a plain session is refused before the service is reached.
func TestUpdateRoutesGating(t *testing.T) {
	releases := fakeReleasesServer(t)
	s, tokens := newUpdateTestServer(t, releases.URL)

	// Check requires authentication.
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/updates/check", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated check status = %d, want 401", rec.Code)
	}

	// A plain session can read availability.
	rec := doRequest(t, s, http.MethodGet, "/api/v1/updates/check", "", "Bearer valid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("check status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var checkBody struct {
		Current   string `json:"current"`
		Available bool   `json:"available"`
		Version   string `json:"version"`
		Notes     string `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &checkBody); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	if !checkBody.Available || checkBody.Version != "v1.2.0" || checkBody.Current != "v1.0.0" {
		t.Fatalf("check body = %+v", checkBody)
	}
	if checkBody.Notes != "" {
		t.Errorf("plain session saw release notes %q", checkBody.Notes)
	}

	// Apply requires a platform operator.
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/updates/apply", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated apply status = %d, want 401", rec.Code)
	}
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/updates/apply", "", "Bearer valid-token"); rec.Code != http.StatusForbidden {
		t.Fatalf("plain-session apply status = %d, want 403", rec.Code)
	}
	for _, scope := range []string{auth.ScopeRead, auth.ScopeDeploy} {
		token, err := tokens.Create(context.Background(), testUserID, "scoped", []string{scope})
		if err != nil {
			t.Fatalf("create %s token: %v", scope, err)
		}
		if rec := doRequest(t, s, http.MethodPost, "/api/v1/updates/apply", "", "Bearer "+token.Token); rec.Code != http.StatusForbidden {
			t.Fatalf("%s-scoped apply status = %d, want 403", scope, rec.Code)
		}
	}

	// An admin token passes the boundary and reaches the service, which refuses
	// to apply without a configured public key (no binary is ever swapped).
	adminToken, err := tokens.Create(context.Background(), testUserID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/updates/apply", "", "Bearer "+adminToken.Token); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin apply status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}

	// A platform operator sees the release notes on the check response.
	adminCheck := doRequest(t, s, http.MethodGet, "/api/v1/updates/check", "", "Bearer "+adminToken.Token)
	if adminCheck.Code != http.StatusOK {
		t.Fatalf("admin check status = %d, want 200", adminCheck.Code)
	}
	var adminBody struct {
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(adminCheck.Body.Bytes(), &adminBody); err != nil {
		t.Fatalf("decode admin check: %v", err)
	}
	if adminBody.Notes != "notes" {
		t.Fatalf("admin notes = %q, want the release notes", adminBody.Notes)
	}
}

// TestUpdateRoutesDisabledUnmounts proves FEATURE_UPDATES=false removes both
// routes.
func TestUpdateRoutesDisabledUnmounts(t *testing.T) {
	releases := fakeReleasesServer(t)
	t.Setenv("FEATURE_UPDATES", "false")
	t.Setenv("GOTHAM_UPDATE_BASE_URL", releases.URL)

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, logger, newFakeAuthService(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/updates/check"},
		{http.MethodPost, "/api/v1/updates/apply"},
	} {
		if rec := doRequest(t, s, tc.method, tc.path, "", "Bearer valid-token"); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}
