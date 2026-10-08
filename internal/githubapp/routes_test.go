package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// testRouter mounts the routes with a fixed user, exercising the full HTTP
// flow end to end against the fake GitHub.
func testRouter(svc *Service, userID uuid.UUID) http.Handler {
	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next },
		func(context.Context) (uuid.UUID, bool) { return userID, true }, svc)
	return r
}

func doRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestRoutesBrowserCallback proves the browser callback needs no bearer
// token: the state identifies the user, and both outcomes finish with a
// redirect to the SPA result route carrying a bounded flag.
func TestRoutesBrowserCallback(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)
	router := testRouter(svc, userID)

	rec := doRequest(t, router, http.MethodPost, "/v1/providers/github-app/manifest",
		map[string]any{"name": "gotham-test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest = %d %s", rec.Code, rec.Body.String())
	}
	var manifest manifestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}

	rec = doRequest(t, router, http.MethodGet,
		"/v1/providers/github-app/callback?code=manifest-code&state="+manifest.State, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("browser callback = %d, want 302", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/applications/github-app/callback?github_app=connected&id=") {
		t.Fatalf("location = %q", location)
	}

	// Replaying the consumed state redirects with the expired flag, and a
	// missing state does too: no JSON error ever reaches the browser.
	rec = doRequest(t, router, http.MethodGet,
		"/v1/providers/github-app/callback?code=manifest-code&state="+manifest.State, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("replay = %d, want 302", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "/applications/github-app/callback?github_app=expired" {
		t.Fatalf("replay location = %q", location)
	}
	rec = doRequest(t, router, http.MethodGet, "/v1/providers/github-app/callback", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("missing state = %d, want 302", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "/applications/github-app/callback?github_app=expired" {
		t.Fatalf("missing state location = %q", location)
	}
}

// TestRoutesInstallStateResolvesApp proves the setup landing can bind an
// installation to the right app when several wait: the pending install state
// resolves to its app without being consumed.
func TestRoutesInstallStateResolvesApp(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)
	router := testRouter(svc, userID)
	ctx := context.Background()

	newApp := func() (string, string) {
		manifest, err := svc.StartManifest(ctx, userID, "", "gotham", "https://gotham.example")
		if err != nil {
			t.Fatal(err)
		}
		app, err := svc.Callback(ctx, userID, "manifest-code", manifest.State)
		if err != nil {
			t.Fatal(err)
		}
		_, state, err := svc.InstallURL(ctx, userID, app.ID)
		if err != nil {
			t.Fatal(err)
		}
		return app.ID.String(), state
	}
	appAID, stateA := newApp()
	appBID, stateB := newApp()

	resolve := func(state string) (int, string) {
		rec := doRequest(t, router, http.MethodGet, "/v1/providers/github-app/install-state?state="+state, nil)
		var body struct {
			AppID string `json:"app_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.AppID
	}
	if code, got := resolve(stateA); code != http.StatusOK || got != appAID {
		t.Fatalf("state A resolves to %q (%d), want %q", got, code, appAID)
	}
	if code, got := resolve(stateB); code != http.StatusOK || got != appBID {
		t.Fatalf("state B resolves to %q (%d), want %q", got, code, appBID)
	}
	if code, _ := resolve("bogus"); code != http.StatusNotFound {
		t.Fatalf("bogus state = %d, want 404", code)
	}
	// Resolution does not consume: both states still record.
	if _, err := svc.RecordInstallation(ctx, userID, mustParseAppID(t, appAID), 999, stateA); err != nil {
		t.Fatal(err)
	}
}

// TestRoutesEndToEnd runs connect + install + list repos + branches + push
// shape + disconnect through HTTP against the fake.

func mustParseAppID(t *testing.T, id string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
func TestRoutesEndToEnd(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)
	api.expiresAt = time.Now().Add(time.Hour)
	router := testRouter(svc, userID)

	rec := doRequest(t, router, http.MethodPost, "/v1/providers/github-app/manifest",
		map[string]any{"name": "gotham-test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest = %d %s", rec.Code, rec.Body.String())
	}
	var manifest manifestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.State == "" || !strings.Contains(manifest.ActionURL, "/settings/apps/new") {
		t.Fatalf("manifest = %+v", manifest)
	}

	rec = doRequest(t, router, http.MethodPost, "/v1/providers/github-app/callback",
		map[string]any{"code": "manifest-code", "state": manifest.State})
	if rec.Code != http.StatusCreated {
		t.Fatalf("callback = %d %s", rec.Code, rec.Body.String())
	}
	var app appResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &app); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Fatal("callback leaks the private key")
	}

	// State replay through HTTP must fail.
	rec = doRequest(t, router, http.MethodPost, "/v1/providers/github-app/callback",
		map[string]any{"code": "manifest-code", "state": manifest.State})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("replay = %d, want 400", rec.Code)
	}

	rec = doRequest(t, router, http.MethodGet, "/v1/providers/github-app/"+app.ID+"/install", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("install = %d %s", rec.Code, rec.Body.String())
	}
	var install installResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &install); err != nil {
		t.Fatal(err)
	}

	rec = doRequest(t, router, http.MethodPost, "/v1/providers/github-app/"+app.ID+"/installations",
		map[string]any{"installation_id": 999, "state": install.State})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record installation = %d %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, router, http.MethodGet, "/v1/providers/github-app/"+app.ID+"/repos", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("repos = %d %s", rec.Code, rec.Body.String())
	}
	var repos repoListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &repos); err != nil {
		t.Fatal(err)
	}
	if len(repos.Repos) != 1 || repos.Repos[0].FullName != "acme/web" {
		t.Fatalf("repos = %+v", repos)
	}

	rec = doRequest(t, router, http.MethodGet, "/v1/providers/github-app/"+app.ID+"/repos/acme/web/branches", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("branches = %d %s", rec.Code, rec.Body.String())
	}
	var branches branchListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &branches); err != nil {
		t.Fatal(err)
	}
	if len(branches.Branches) != 2 {
		t.Fatalf("branches = %+v", branches)
	}

	rec = doRequest(t, router, http.MethodGet, "/v1/providers/github-app", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	// GS-10 lists the connection age: the wire shape carries created_at.
	if !strings.Contains(rec.Body.String(), `"created_at"`) {
		t.Fatalf("list misses created_at: %s", rec.Body.String())
	}

	rec = doRequest(t, router, http.MethodDelete, "/v1/providers/github-app/"+app.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect = %d %s", rec.Code, rec.Body.String())
	}
	var disconnected disconnectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &disconnected); err != nil {
		t.Fatal(err)
	}
	if !disconnected.Deleted {
		t.Fatalf("disconnect = %+v", disconnected)
	}

	// Unknown app answers 404.
	rec = doRequest(t, router, http.MethodGet, "/v1/providers/github-app/"+uuid.NewString()+"/repos", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown app = %d, want 404", rec.Code)
	}
}
