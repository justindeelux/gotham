package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// alwaysUser returns a UserIDFunc for one user.
func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

// newRouteServer mounts the project routes with an auth middleware that
// injects the given team scope, standing in for the server's RequireAuth +
// RequireTeam chain (without the write gate, so the service's own role
// check is what answers 403 here).
func newRouteServer(userID, teamID uuid.UUID, role teams.Role, svc ProjectService) http.Handler {
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := teams.Scope{UserID: userID, TeamID: teamID, Role: role}
			next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
		})
	}
	router := chi.NewRouter()
	Mount(router, auth, alwaysUser(userID), svc)
	return router
}

// doRequest runs one request against the router and returns the recorder.
func doRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// errorMessage decodes the {"message": ...} failure body.
func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Message
}

// TestProjectRoutesLifecycle walks the contract's project and environment
// routes and asserts the exact response shapes.
func TestProjectRoutesLifecycle(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := NewService(Config{Repository: newFakeRepository(), Counter: newFakeCounter(), Logger: discardLogger()})
	handler := newRouteServer(userID, teamID, teams.RoleAdmin, svc)

	rec := doRequest(handler, http.MethodPost, "/v1/projects", `{"name":"Shop","description":"storefront"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s), want 201", rec.Code, rec.Body.String())
	}
	var created projectCreateEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Project.Name != "Shop" || created.Project.Description != "storefront" {
		t.Errorf("project = %+v, want Shop/storefront", created.Project)
	}
	if len(created.Environments) != 1 || created.Environments[0].Name != ProductionEnvironment {
		t.Fatalf("environments = %+v, want one production", created.Environments)
	}
	if created.Project.EnvironmentCount != 1 {
		t.Errorf("environment_count = %d, want 1", created.Project.EnvironmentCount)
	}
	projectID := created.Project.ID
	productionID := created.Environments[0].ID

	rec = doRequest(handler, http.MethodGet, "/v1/projects", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var listed projectListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].ID != projectID {
		t.Fatalf("projects = %+v, want the created one", listed.Projects)
	}

	rec = doRequest(handler, http.MethodGet, "/v1/projects/"+projectID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	var detailed projectDetailEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &detailed); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if len(detailed.Environments) != 1 {
		t.Fatalf("environments = %+v, want one", detailed.Environments)
	}

	rec = doRequest(handler, http.MethodPost, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("env create = %d (body %s), want 201", rec.Code, rec.Body.String())
	}
	var envCreated environmentEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envCreated); err != nil {
		t.Fatalf("decode env create: %v", err)
	}
	if envCreated.Environment.ProjectID != projectID {
		t.Errorf("project_id = %q, want %q", envCreated.Environment.ProjectID, projectID)
	}
	stagingID := envCreated.Environment.ID

	rec = doRequest(handler, http.MethodPatch, "/v1/projects/"+projectID, `{"description":"new blurb"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d (body %s)", rec.Code, rec.Body.String())
	}
	var patched projectEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patched.Project.Description != "new blurb" || patched.Project.Name != "Shop" {
		t.Errorf("project = %+v, want renamed description only", patched.Project)
	}

	rec = doRequest(handler, http.MethodPatch, "/v1/environments/"+stagingID, `{"name":"qa"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("env patch = %d (body %s)", rec.Code, rec.Body.String())
	}

	// The last environment cannot be deleted: remove staging, then the
	// production delete is refused.
	rec = doRequest(handler, http.MethodDelete, "/v1/environments/"+stagingID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("env delete = %d (body %s), want 204", rec.Code, rec.Body.String())
	}
	rec = doRequest(handler, http.MethodDelete, "/v1/environments/"+productionID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete last = %d, want 409", rec.Code)
	}
	if message := errorMessage(t, rec); message != "a project needs at least one environment" {
		t.Fatalf("last-environment body = %q, want the contract text", message)
	}

	rec = doRequest(handler, http.MethodDelete, "/v1/projects/"+projectID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("project delete = %d (body %s), want 204", rec.Code, rec.Body.String())
	}
	rec = doRequest(handler, http.MethodGet, "/v1/projects/"+projectID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", rec.Code)
	}
	if message := errorMessage(t, rec); message != "not found" {
		t.Fatalf("missing body = %q, want not found", message)
	}
}

// TestProjectRoutesConflicts asserts the contract's exact 409 bodies.
func TestProjectRoutesConflicts(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := NewService(Config{Repository: newFakeRepository(), Counter: newFakeCounter(), Logger: discardLogger()})
	handler := newRouteServer(userID, teamID, teams.RoleAdmin, svc)

	rec := doRequest(handler, http.MethodPost, "/v1/projects", `{"name":"Shop"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	rec = doRequest(handler, http.MethodPost, "/v1/projects", `{"name":"SHOP"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d, want 409", rec.Code)
	}
	if message := errorMessage(t, rec); message != "project name already exists" {
		t.Fatalf("duplicate body = %q, want the contract text", message)
	}

	rec = doRequest(handler, http.MethodGet, "/v1/projects", "")
	var listed projectListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Projects) != 1 {
		t.Fatalf("projects = %+v, want one", listed.Projects)
	}
	projectID := listed.Projects[0].ID

	rec = doRequest(handler, http.MethodPost, "/v1/projects/"+projectID+"/environments", `{"name":"PRODUCTION"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate env = %d, want 409", rec.Code)
	}
	if message := errorMessage(t, rec); message != "environment name already exists" {
		t.Fatalf("duplicate env body = %q, want the contract text", message)
	}
}

// TestProjectRoutesAuthorization asserts viewer 403s and cross-team 404s.
func TestProjectRoutesAuthorization(t *testing.T) {
	owner, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc := NewService(Config{Repository: repo, Counter: newFakeCounter(), Logger: discardLogger()})
	admin := newRouteServer(owner, teamID, teams.RoleAdmin, svc)

	rec := doRequest(admin, http.MethodPost, "/v1/projects", `{"name":"Shop"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	var created projectCreateEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	viewerID := uuid.New()
	viewer := newRouteServer(viewerID, teamID, teams.RoleReadOnly, svc)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/projects", `{"name":"Other"}`},
		{http.MethodPatch, "/v1/projects/" + created.Project.ID, `{"name":"x"}`},
		{http.MethodDelete, "/v1/projects/" + created.Project.ID, ""},
		{http.MethodPost, "/v1/projects/" + created.Project.ID + "/environments", `{"name":"staging"}`},
		{http.MethodPatch, "/v1/environments/" + created.Environments[0].ID, `{"name":"x"}`},
		{http.MethodDelete, "/v1/environments/" + created.Environments[0].ID, ""},
	} {
		rec := doRequest(viewer, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as viewer = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
	// Viewer reads stay open.
	if rec := doRequest(viewer, http.MethodGet, "/v1/projects", ""); rec.Code != http.StatusOK {
		t.Errorf("viewer list = %d, want 200", rec.Code)
	}
	if rec := doRequest(viewer, http.MethodGet, "/v1/projects/"+created.Project.ID, ""); rec.Code != http.StatusOK {
		t.Errorf("viewer get = %d, want 200", rec.Code)
	}

	stranger := newRouteServer(uuid.New(), uuid.New(), teams.RoleAdmin, svc)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/projects/" + created.Project.ID, ""},
		{http.MethodPatch, "/v1/projects/" + created.Project.ID, `{"name":"x"}`},
		{http.MethodDelete, "/v1/projects/" + created.Project.ID, ""},
		{http.MethodGet, "/v1/projects/" + created.Project.ID + "/environments", ""},
		{http.MethodPost, "/v1/projects/" + created.Project.ID + "/environments", `{"name":"staging"}`},
		{http.MethodPatch, "/v1/environments/" + created.Environments[0].ID, `{"name":"x"}`},
		{http.MethodDelete, "/v1/environments/" + created.Environments[0].ID, ""},
	} {
		rec := doRequest(stranger, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s cross-team = %d, want 404", tc.method, tc.path, rec.Code)
		}
		if message := errorMessage(t, rec); message != "not found" {
			t.Errorf("%s %s cross-team body = %q, want not found", tc.method, tc.path, message)
		}
	}
	// A stranger's list is empty, not forbidden: team isolation, not an
	// error.
	if rec := doRequest(stranger, http.MethodGet, "/v1/projects", ""); rec.Code != http.StatusOK {
		t.Errorf("stranger list = %d, want 200", rec.Code)
	}
}

// TestProjectRoutesBadInput asserts malformed IDs and bodies answer 400.
func TestProjectRoutesBadInput(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := NewService(Config{Repository: newFakeRepository(), Counter: newFakeCounter(), Logger: discardLogger()})
	handler := newRouteServer(userID, teamID, teams.RoleAdmin, svc)

	if rec := doRequest(handler, http.MethodGet, "/v1/projects/nope", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad project id = %d, want 400", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPatch, "/v1/environments/nope", `{"name":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad environment id = %d, want 400", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPost, "/v1/projects", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("empty body = %d, want 400", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPost, "/v1/projects", `{"name":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty name = %d, want 400", rec.Code)
	}
}

// TestMountNilServiceMountsNothing asserts the server can call Mount
// unconditionally when there is no database.
func TestMountNilServiceMountsNothing(t *testing.T) {
	router := chi.NewRouter()
	Mount(router, func(next http.Handler) http.Handler { return next }, alwaysUser(uuid.New()), nil)
	rec := doRequest(router, http.MethodGet, "/v1/projects", "")
	if rec.Code == http.StatusOK {
		t.Fatal("nil service mounted routes")
	}
}
