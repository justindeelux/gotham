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

// TestRoutesEndToEnd runs connect + install + list repos + branches + push
// shape + disconnect through HTTP against the fake.
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
