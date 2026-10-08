package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// applicationsPath is the collection route.
const applicationsPath = "/v1/applications"

// applicationBody is the create payload exactly as FE-4.1's wizard sends it
// (see CreateApplicationInput in web/src/features/applications/api/applications.ts).
const applicationBody = `{
	"name": "demo app",
	"provider": "github",
	"repo": "acme/demo",
	"clone_url": "https://github.com/acme/demo.git",
	"branch": "main",
	"build_pack": "dockerfile",
	"base_domain": "demo.example.com",
	"port": 3000,
	"host_port": 8080,
	"environment_id": "%s",
	"server_id": "%s",
	"env": [
		{"key": "NODE_ENV", "value": "production"},
		{"key": "API_TOKEN", "value": "secret:super-secret"}
	],
	"storage": [
		{"name": "data", "host_path": "", "container_path": "/var/lib/app"}
	]
}`

// applicationWireKeys are the fields of the FE's `Application` interface plus
// the additive `base_domain_disabled` visibility flag and the `github_app_id`
// link; the envelope must carry exactly these, or the SPA reads undefined
// values.
var applicationWireKeys = []string{
	"id", "name", "environment_id", "environment_name", "project_id",
	"project_name", "provider", "repo", "clone_url", "source_type", "github_app_id",
	"dockerfile_content", "build_args", "branch", "build_pack",
	"base_domain", "base_domain_disabled", "port", "host_port", "server_id",
	"server_name", "created_at", "updated_at",
}

// applicationListWireKeys are the list-item fields: the Dockerfile source
// text and --build-arg values travel on the detail routes only, so a list
// read never exposes them.
var applicationListWireKeys = []string{
	"id", "name", "environment_id", "environment_name", "project_id",
	"project_name", "provider", "repo", "clone_url", "source_type",
	"branch", "build_pack",
	"base_domain", "base_domain_disabled", "port", "host_port", "server_id",
	"server_name", "created_at", "updated_at",
}

// assertJSONKeys fails unless body is an object with exactly the given keys.
func assertJSONKeys(t *testing.T, body []byte, want ...string) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body %s: %v", body, err)
	}
	if len(payload) != len(want) {
		t.Errorf("keys = %v, want %v", keysOf(payload), want)
	}
	for _, key := range want {
		if _, ok := payload[key]; !ok {
			t.Errorf("missing key %q in %s", key, body)
		}
	}
}

// keysOf renders a decoded object's keys for failure output.
func keysOf(payload map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	return keys
}

// sampleApplication returns an application with every wire field filled.
func sampleApplication() Application {
	return Application{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		ServerID:      uuid.New(),
		EnvironmentID: uuid.New(),
		Name:          "demo app",
		Provider:      "github",
		Repo:          "acme/demo",
		CloneURL:      "https://github.com/acme/demo.git",
		Branch:        "main",
		BuildPack:     "dockerfile",
		BaseDomain:    "demo.example.com",
		Port:          3000,
		HostPort:      8080,
	}
}

func TestRoutesCreateApplication(t *testing.T) {
	userID, serverID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.UserID = userID
	svc := &fakeDeployService{application: app}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
		strings.NewReader(strings.Replace(strings.Replace(applicationBody, "%s", uuid.New().String(), 1), "%s", serverID.String(), 1))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	assertJSONKeys(t, rec.Body.Bytes(), "application")
	assertJSONKeys(t, mustJSON(t, rec.Body.Bytes(), "application"), applicationWireKeys...)

	if svc.seenUser != userID {
		t.Errorf("service saw user %s, want %s", svc.seenUser, userID)
	}
	in := svc.seenCreate
	if in.Name != "demo app" || in.CloneURL != "https://github.com/acme/demo.git" {
		t.Errorf("input = %+v, want the parsed create payload", in)
	}
	if in.ServerID != serverID {
		t.Errorf("server_id = %s, want %s", in.ServerID, serverID)
	}
	if in.Port != 3000 || in.HostPort != 8080 {
		t.Errorf("ports = %d/%d, want 3000/8080", in.Port, in.HostPort)
	}
	if len(in.Env) != 2 || in.Env[1].Key != "API_TOKEN" || in.Env[1].Value != "secret:super-secret" {
		t.Errorf("env = %+v, want the nested environment as sent", in.Env)
	}
	if len(in.Storage) != 1 || in.Storage[0].ContainerPath != "/var/lib/app" {
		t.Errorf("storage = %+v, want the nested volume map as sent", in.Storage)
	}
}

func TestRoutesCreateApplicationRejectsBadInput(t *testing.T) {
	userID, serverID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.UserID = userID
	valid := strings.Replace(strings.Replace(applicationBody, "%s", uuid.New().String(), 1), "%s", serverID.String(), 1)

	cases := []struct {
		name string
		body func() string
		want int
	}{
		{"empty body", func() string { return "" }, http.StatusBadRequest},
		{"unknown field", func() string {
			return strings.TrimSuffix(valid, "}") + `,"nope":1}`
		}, http.StatusBadRequest},
		{"malformed json", func() string { return "{" }, http.StatusBadRequest},
		{"invalid server id", func() string {
			return strings.Replace(valid, serverID.String(), "not-a-uuid", 1)
		}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeDeployService{application: app}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath, strings.NewReader(tc.body())))

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestRoutesCreateApplicationInstallsProviderHook pins the BE-4.4 create
// lifecycle: an application created with a supported provider and a repository
// installs its push hook through the service, which is what lets a push
// trigger auto-deploys without the caller touching the webhook route. The
// response reports the successful install.
func TestRoutesCreateApplicationInstallsProviderHook(t *testing.T) {
	userID, serverID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.UserID = userID
	svc := &fakeDeployService{application: app, installAttempted: true}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
		strings.NewReader(strings.Replace(strings.Replace(applicationBody, "%s", uuid.New().String(), 1), "%s", serverID.String(), 1))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.installCalls != 1 {
		t.Fatalf("hook installs = %d, want 1", svc.installCalls)
	}
	if svc.installedFor != app.ID || svc.seenUser != userID {
		t.Errorf("install saw app %s / user %s, want %s / %s",
			svc.installedFor, svc.seenUser, app.ID, userID)
	}
	var body applicationEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Webhook == nil || !body.Webhook.Installed || body.Webhook.Error != "" {
		t.Errorf("webhook outcome = %+v, want installed with no error", body.Webhook)
	}
}

// TestRoutesCreateApplicationSkipsHookWithoutProvider pins the other half: a
// pasted public URL (provider "public" or empty) has no provider hook, so the
// route must not call the lifecycle at all.
func TestRoutesCreateApplicationSkipsHookWithoutProvider(t *testing.T) {
	for _, provider := range []string{"", "public", "manual"} {
		t.Run("provider "+provider, func(t *testing.T) {
			userID, serverID := uuid.New(), uuid.New()
			app := sampleApplication()
			app.UserID, app.Provider = userID, provider
			svc := &fakeDeployService{application: app}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
				strings.NewReader(strings.Replace(strings.Replace(applicationBody, "%s", uuid.New().String(), 1), "%s", serverID.String(), 1))))

			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
			}
			if svc.installCalls != 0 {
				t.Errorf("hook installs = %d, want 0", svc.installCalls)
			}
		})
	}
}

// TestRoutesCreateApplicationSurvivesHookFailure pins the failure semantics:
// the application row is already committed, so a provider outage, missing
// credentials or an unusable callback cannot turn the create into an error
// (the caller would retry and create a duplicate). The failure is logged by
// the service and the response carries a coarse webhook outcome so the caller
// knows automatic deploys are off and which route retries the install.
func TestRoutesCreateApplicationSurvivesHookFailure(t *testing.T) {
	userID, serverID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.UserID = userID
	svc := &fakeDeployService{
		application:      app,
		installAttempted: true,
		installErr:       errors.New("provider unavailable"),
	}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
		strings.NewReader(strings.Replace(strings.Replace(applicationBody, "%s", uuid.New().String(), 1), "%s", serverID.String(), 1))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body applicationEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Application.ID != app.ID.String() {
		t.Errorf("application id = %q, want %q", body.Application.ID, app.ID)
	}
	if svc.installCalls != 1 {
		t.Errorf("hook installs = %d, want the one failed attempt", svc.installCalls)
	}
	if body.Webhook == nil || body.Webhook.Installed {
		t.Fatalf("webhook outcome = %+v, want installed=false", body.Webhook)
	}
	if !strings.Contains(body.Webhook.Error, "retry") {
		t.Errorf("webhook error = %q, want the retry named", body.Webhook.Error)
	}
	if strings.Contains(rec.Body.String(), "provider unavailable") {
		t.Error("the provider failure leaked into the response body")
	}
}

// TestRoutesCreateApplicationBoundsStalledHookProvider drives the real service
// through the create route with a provider that accepts the connection and
// then stalls: the request must return inside the hook timeout with 201 and
// the outcome must be recorded with the configured logger. Without the bound
// the SPA would time out after the row committed and invite a duplicate
// create (MEDIUM-1).
func TestRoutesCreateApplicationBoundsStalledHookProvider(t *testing.T) {
	userID, serverID := uuid.New(), uuid.New()
	stall := &stallingHookLifecycle{}

	var logs bytes.Buffer
	svc := NewService(Config{
		Repository:  &fakeRepository{},
		Secret:      testSecretKey,
		Logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		Hooks:       func() HookLifecycle { return stall },
		HookTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = svc.Close() })
	srv := newRouteServer(svc, alwaysUser(userID))

	start := time.Now()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
		strings.NewReader(strings.Replace(strings.Replace(applicationBody, "%s", uuid.New().String(), 1), "%s", serverID.String(), 1))))
	elapsed := time.Since(start)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("create took %s, want it bounded by the 50ms hook timeout", elapsed)
	}
	var body applicationEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Application.ID == "" {
		t.Error("the stalled hook install swallowed the created application")
	}
	if body.Webhook == nil || body.Webhook.Installed {
		t.Errorf("webhook outcome = %+v, want installed=false for the stalled provider", body.Webhook)
	}
	if stall.installCalls != 1 {
		t.Errorf("hook installs = %d, want 1", stall.installCalls)
	}
	if logged := logs.String(); !strings.Contains(logged, "webhook not installed") {
		t.Errorf("log = %q, want the stalled install recorded", logged)
	}
}

func TestRoutesListApplications(t *testing.T) {
	userID := uuid.New()
	app := sampleApplication()
	app.UserID = userID

	cases := []struct {
		name string
		apps []Application
	}{
		{"empty", nil},
		{"one", []Application{app}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeDeployService{listApps: tc.apps}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			assertJSONKeys(t, rec.Body.Bytes(), "applications")

			var body applicationListEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Applications == nil {
				t.Fatal("applications = null, want an empty array the SPA can map over")
			}
			if len(body.Applications) != len(tc.apps) {
				t.Fatalf("applications = %d, want %d", len(body.Applications), len(tc.apps))
			}
			if len(tc.apps) == 1 {
				assertJSONKeys(t, mustJSON(t, rec.Body.Bytes(), "applications", "0"), applicationListWireKeys...)
				if body.Applications[0].ID != app.ID.String() {
					t.Errorf("id = %q, want %q", body.Applications[0].ID, app.ID)
				}
			}
			if svc.seenUser != userID {
				t.Errorf("service saw user %s, want %s", svc.seenUser, userID)
			}
		})
	}

	t.Run("dockerfile fields stay on the detail routes", func(t *testing.T) {
		dockerApp := sampleApplication()
		dockerApp.UserID = userID
		dockerApp.SourceType = SourceDockerfile
		dockerApp.DockerfileContent = "FROM alpine:3.20\n"
		dockerApp.BuildArgs = map[string]string{"APP_ENV": "production"}
		svc := &fakeDeployService{listApps: []Application{dockerApp}}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		raw := mustJSON(t, rec.Body.Bytes(), "applications", "0")
		var keys map[string]any
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := keys["dockerfile_content"]; ok {
			t.Error("list response carries dockerfile_content, want it detail-only")
		}
		if _, ok := keys["build_args"]; ok {
			t.Error("list response carries build_args, want them detail-only")
		}
	})
}

func TestRoutesGetApplication(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.ID = appID
	svc := &fakeDeployService{application: app}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath+"/"+appID.String(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	assertJSONKeys(t, rec.Body.Bytes(), "application")
	assertJSONKeys(t, mustJSON(t, rec.Body.Bytes(), "application"), applicationWireKeys...)
	if svc.seenApplication != appID {
		t.Errorf("service saw application %s, want %s", svc.seenApplication, appID)
	}
}

func TestRoutesApplicationNotFound(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{getErr: ErrNotFound}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath+"/"+appID.String(), nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for another user's application", rec.Code)
	}
}

func TestRoutesNullServerID(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.ID, app.ServerID = appID, uuid.Nil
	svc := &fakeDeployService{application: app}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath+"/"+appID.String(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body applicationEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Application.ServerID != nil {
		t.Errorf("server_id = %q, want null when no node is assigned", *body.Application.ServerID)
	}
}

func TestRoutesUpdateApplication(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	app := sampleApplication()
	app.ID = appID

	cases := []struct {
		name string
		body string
		want int
	}{
		{"partial patch", `{"name":"renamed","branch":"release","port":9090}`, http.StatusOK},
		{"empty object", `{}`, http.StatusOK},
		{"no body", ``, http.StatusBadRequest},
		{"unknown field", `{"nope":1}`, http.StatusBadRequest},
		{"invalid port", `{"port":"8080"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeDeployService{application: app}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut,
				applicationsPath+"/"+appID.String(), strings.NewReader(tc.body)))

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if rec.Code != http.StatusOK {
				return
			}
			assertJSONKeys(t, rec.Body.Bytes(), "application")
			if svc.seenApplication != appID {
				t.Errorf("service saw application %s, want %s", svc.seenApplication, appID)
			}
		})
	}

	t.Run("forwards the patched fields", func(t *testing.T) {
		svc := &fakeDeployService{application: app}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+appID.String(),
			strings.NewReader(`{"name":"renamed","branch":"release","port":9090}`)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		in := svc.seenUpdate
		if in.Name == nil || *in.Name != "renamed" {
			t.Errorf("name = %v, want the patched value", in.Name)
		}
		if in.Branch == nil || *in.Branch != "release" {
			t.Errorf("branch = %v, want the patched value", in.Branch)
		}
		if in.Port == nil || *in.Port != 9090 {
			t.Errorf("port = %v, want the patched value", in.Port)
		}
		if in.HostPort != nil || in.BuildPack != nil || in.ServerID != nil {
			t.Errorf("unpatched fields were sent: %+v", in)
		}
	})

	// The link round-trips through the update body: a UUID sets it, an
	// empty string clears it, absent leaves it alone.
	t.Run("link set and clear", func(t *testing.T) {
		linkID := uuid.NewString()
		for _, tc := range []struct {
			name string
			body string
			want *string
		}{
			{"set link", `{"github_app_id":"` + linkID + `"}`, &linkID},
			{"clear link", `{"github_app_id":""}`, strPtr("")},
			{"absent link", `{"name":"renamed"}`, nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				svc := &fakeDeployService{application: app}
				srv := newRouteServer(svc, alwaysUser(userID))

				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+appID.String(),
					strings.NewReader(tc.body)))

				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
				}
				got := svc.seenUpdate.GitHubAppID
				if tc.want == nil {
					if got != nil {
						t.Errorf("link = %q, want unchanged (nil)", *got)
					}
					return
				}
				if got == nil || *got != *tc.want {
					t.Errorf("link = %v, want %q", got, *tc.want)
				}
			})
	t.Run("forwards dockerfile fields", func(t *testing.T) {
		svc := &fakeDeployService{application: app}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+appID.String(),
			strings.NewReader(`{"dockerfile_content":"FROM alpine:3.21\n","build_args":{"APP_ENV":"staging"}}`)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		in := svc.seenUpdate
		if in.DockerfileContent == nil || *in.DockerfileContent != "FROM alpine:3.21\n" {
			t.Errorf("dockerfile content = %v, want the patched value", in.DockerfileContent)
		}
		if in.BuildArgs == nil || (*in.BuildArgs)["APP_ENV"] != "staging" {
			t.Errorf("build args = %v, want APP_ENV=staging", in.BuildArgs)
		}
		if in.Name != nil || in.Branch != nil {
			t.Errorf("unpatched fields were sent: %+v", in)
		}
	})
}

func strPtr(s string) *string { return &s }

func TestRoutesDeleteApplication(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, applicationsPath+"/"+appID.String(), nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want it empty", rec.Body.String())
	}
	if svc.seenUser != userID || svc.seenApplication != appID {
		t.Errorf("service saw %s/%s, want %s/%s", svc.seenUser, svc.seenApplication, userID, appID)
	}
}

// TestRoutesDeleteApplicationFailsClosedOnHookFailure drives the real service:
// when the provider hook cannot be removed, the delete answers 502 and the
// application survives. The stored hook row is the only handle on the remote
// hook, so the caller retries, cleans the host by hand, or force-forgets the
// row (DELETE .../webhooks?force=true) and deletes again.
func TestRoutesDeleteApplicationFailsClosedOnHookFailure(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	hooks := &fakeHookLifecycle{removeErr: fmt.Errorf("%w: provider unavailable", ErrProvider)}
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Hooks:      func() HookLifecycle { return hooks },
	})
	t.Cleanup(func() { _ = svc.Close() })
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, applicationsPath+"/"+app.ID.String(), nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}
	if hooks.removeCalls != 1 {
		t.Errorf("hook removals = %d, want the one failed attempt", hooks.removeCalls)
	}
	if _, err := svc.GetApplication(context.Background(), userID, app.ID); err != nil {
		t.Errorf("the application must survive a failed hook removal: %v", err)
	}
}

func TestRoutesEnvCollection(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	reference := secretRefPrefix + uuid.New().String()
	svc := &fakeDeployService{env: []EnvEntry{
		{Key: "API_TOKEN", Value: reference},
		{Key: "NODE_ENV", Value: "production"},
	}}
	srv := newRouteServer(svc, alwaysUser(userID))
	path := applicationsPath + "/" + appID.String() + "/env"

	t.Run("read", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, rec.Body.Bytes(), "env")

		var body envListEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Env) != 2 {
			t.Fatalf("env = %+v, want both rows", body.Env)
		}
		if body.Env[0].Value != reference {
			t.Errorf("secret value = %q, want the reference, never the plaintext", body.Env[0].Value)
		}
	})

	t.Run("replace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body := `{"env":[{"key":"API_TOKEN","value":"` + reference + `"},{"key":"LOG_LEVEL","value":"debug"}]}`
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, rec.Body.Bytes(), "env")
		if len(svc.seenEntries) != 2 || svc.seenEntries[1].Key != "LOG_LEVEL" {
			t.Errorf("entries = %+v, want the collection as sent", svc.seenEntries)
		}
		if svc.seenApplication != appID {
			t.Errorf("service saw application %s, want %s", svc.seenApplication, appID)
		}
	})

	t.Run("empty body does not clear the collection", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader("")))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"nope":1}`)))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestRoutesStorageCollection(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{storages: []Storage{
		{Name: "data", HostPath: "/data/app", ContainerPath: "/var/lib/app"},
	}}
	srv := newRouteServer(svc, alwaysUser(userID))
	path := applicationsPath + "/" + appID.String() + "/storages"

	t.Run("read", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, rec.Body.Bytes(), "storage")

		var body storageListEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Storage) != 1 || body.Storage[0].Name != "data" {
			t.Errorf("storage = %+v, want the stored mapping", body.Storage)
		}
	})

	t.Run("replace", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body := `{"storage":[{"name":"cache","host_path":"/data/cache","container_path":"/var/cache"}]}`
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, rec.Body.Bytes(), "storage")
		if len(svc.seenStorages) != 1 || svc.seenStorages[0].Name != "cache" {
			t.Errorf("storages = %+v, want the collection as sent", svc.seenStorages)
		}
	})

	t.Run("empty body does not clear the collection", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader("")))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestRoutesStopStart(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	deployment := Deployment{
		ID:            uuid.New(),
		ApplicationID: appID,
		Kind:          KindDeploy,
		State:         StateRunning,
		ContainerID:   "abc123",
	}

	t.Run("stop answers the deployment", func(t *testing.T) {
		svc := &fakeDeployService{stop: deployment}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath+"/"+appID.String()+"/stop", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, rec.Body.Bytes(), "deployment")
		// The deployment envelope carries omitempty fields (image_tag, error,
		// timestamps) exactly like the routes FE-4.1 already consumes; these
		// are the keys every answer has.
		assertJSONKeys(t, mustJSON(t, rec.Body.Bytes(), "deployment"),
			"id", "application_id", "kind", "state", "attempt", "container_id",
			"created_at", "updated_at")
		if svc.seenApplication != appID {
			t.Errorf("service saw application %s, want %s", svc.seenApplication, appID)
		}
	})

	t.Run("start answers the deployment", func(t *testing.T) {
		svc := &fakeDeployService{start: deployment}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath+"/"+appID.String()+"/start", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, rec.Body.Bytes(), "deployment")
	})

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"server not found", ErrServerNotFound, http.StatusNotFound},
		{"conflict", ErrConflict, http.StatusConflict},
		{"agent unavailable", ErrAgentUnavailable, http.StatusBadGateway},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		for _, action := range []string{"stop", "start"} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				svc := &fakeDeployService{stopErr: tc.err, startErr: tc.err}
				srv := newRouteServer(svc, alwaysUser(userID))

				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
					applicationsPath+"/"+appID.String()+"/"+action, nil))

				if rec.Code != tc.want {
					t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
				}
			})
		}
	}
}

func TestRoutesApplicationServiceErrors(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{
		createErr:   ErrValidation,
		listAppsErr: ErrValidation,
		getErr:      ErrValidation,
		updateErr:   ErrValidation,
		deleteErr:   ErrValidation,
		envErr:      ErrValidation,
		storagesErr: ErrValidation,
	}
	srv := newRouteServer(svc, alwaysUser(userID))
	application := applicationsPath + "/" + appID.String()

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, applicationsPath, `{"name":"demo","clone_url":"https://example.com/a.git","server_id":"` + uuid.New().String() + `"}`},
		{"list", http.MethodGet, applicationsPath, ""},
		{"get", http.MethodGet, application, ""},
		{"update", http.MethodPut, application, `{"name":"renamed"}`},
		{"delete", http.MethodDelete, application, ""},
		{"env read", http.MethodGet, application + "/env", ""},
		{"env replace", http.MethodPut, application + "/env", `{"env":[{"key":"A","value":"b"}]}`},
		{"storages read", http.MethodGet, application + "/storages", ""},
		{"storages replace", http.MethodPut, application + "/storages", `{"storage":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, body))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRoutesNewApplicationRoutesGuardAccess(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{}
	unauthenticated := newRouteServer(svc, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false })

	guards := []struct {
		method string
		path   string
	}{
		{http.MethodPost, applicationsPath},
		{http.MethodGet, applicationsPath},
		{http.MethodGet, applicationsPath + "/" + appID.String()},
		{http.MethodPut, applicationsPath + "/" + appID.String()},
		{http.MethodDelete, applicationsPath + "/" + appID.String()},
		{http.MethodGet, applicationsPath + "/" + appID.String() + "/env"},
		{http.MethodPut, applicationsPath + "/" + appID.String() + "/env"},
		{http.MethodGet, applicationsPath + "/" + appID.String() + "/storages"},
		{http.MethodPut, applicationsPath + "/" + appID.String() + "/storages"},
		{http.MethodPost, applicationsPath + "/" + appID.String() + "/stop"},
		{http.MethodPost, applicationsPath + "/" + appID.String() + "/start"},
	}
	for _, guard := range guards {
		t.Run(guard.method+" "+guard.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			unauthenticated.ServeHTTP(rec, httptest.NewRequest(guard.method, guard.path, nil))

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}

	t.Run("invalid application id", func(t *testing.T) {
		srv := newRouteServer(svc, alwaysUser(userID))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath+"/not-a-uuid", nil))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

// mustJSON walks a JSON document along the given path (object keys and array
// indexes as strings) and returns the raw slice at that location.
func mustJSON(t *testing.T, body []byte, path ...string) []byte {
	t.Helper()
	var node any
	if err := json.Unmarshal(body, &node); err != nil {
		t.Fatalf("decode body %s: %v", body, err)
	}
	current := node
	for _, segment := range path {
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[segment]
			if !ok {
				t.Fatalf("path segment %q missing in %s", segment, body)
			}
			current = value
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil {
				t.Fatalf("invalid array index %q: %v", segment, err)
			}
			if index < 0 || index >= len(typed) {
				t.Fatalf("array index %d out of range in %s", index, body)
			}
			current = typed[index]
		default:
			t.Fatalf("cannot descend into %T at %q", current, segment)
		}
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	return encoded
}

// TestRoutesCreateApplicationSourceTypes posts source-typed payloads through
// the real service: an unknown type is a 400, a not-yet-implemented type is
// a 422, a provider/type mismatch is a 400, and a matching github_app
// creates with the stored type.
func TestRoutesCreateApplicationSourceTypes(t *testing.T) {
	body := func(sourceType, provider, repo, cloneURL string) string {
		payload := map[string]any{
			"name":           "demo app",
			"environment_id": uuid.New().String(),
			"provider":       provider,
			"repo":           repo,
			"clone_url":      cloneURL,
			"source_type":    sourceType,
			"branch":         "main",
			"build_pack":     "dockerfile",
			"server_id":      uuid.New().String(),
		}
		if sourceType == SourceDockerfile {
			payload["dockerfile_content"] = "FROM alpine:3.20\n"
			payload["build_args"] = map[string]string{"APP_ENV": "production"}
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode payload: %v", err)
		}
		return string(encoded)
	}
	cases := []struct {
		name       string
		sourceType string
		provider   string
		want       int
	}{
		{"unknown type", "tarball", "", http.StatusBadRequest},
		{"private git waits for GS-4", SourceGitPrivate, "", http.StatusUnprocessableEntity},
		{"github app with gitlab provider", SourceGitHubApp, "gitlab", http.StatusBadRequest},
		{"public git with github provider", SourceGitPublic, "github", http.StatusBadRequest}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			svc := newTestService(t, &fakeRepository{})
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
				strings.NewReader(body(tc.sourceType, tc.provider, "acme/demo", "https://github.com/acme/demo.git"))))

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	t.Run("dockerfile app creates with content and build args", func(t *testing.T) {
		userID := uuid.New()
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
			strings.NewReader(body(SourceDockerfile, "", "", ""))))

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
		var envelope struct {
			Application struct {
				SourceType        string            `json:"source_type"`
				DockerfileContent string            `json:"dockerfile_content"`
				BuildArgs         map[string]string `json:"build_args"`
			} `json:"application"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Application.SourceType != SourceDockerfile {
			t.Errorf("source type = %q, want %q", envelope.Application.SourceType, SourceDockerfile)
		}
		if !strings.Contains(envelope.Application.DockerfileContent, "FROM") {
			t.Errorf("dockerfile content = %q, want the pasted text", envelope.Application.DockerfileContent)
		}
		if envelope.Application.BuildArgs["APP_ENV"] != "production" {
			t.Errorf("build args = %v, want APP_ENV=production", envelope.Application.BuildArgs)
		}
	})

	t.Run("matching github app creates", func(t *testing.T) {
		userID := uuid.New()
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
			strings.NewReader(body(SourceGitHubApp, "github", "acme/demo", "https://github.com/acme/demo.git"))))

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
		var envelope struct {
			Application struct {
				Provider   string `json:"provider"`
				SourceType string `json:"source_type"`
			} `json:"application"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Application.SourceType != SourceGitHubApp || envelope.Application.Provider != "github" {
			t.Errorf("application = %+v, want github_app/github", envelope.Application)
		}
	})

	// A github_app application keeps the legacy validation: provider=github
	// applications predate GitHub App connections (OAuth flow, deploy keys),
	// so any clone URL the cloner supports stays creatable. The token path
	// applies at clone time only when an installation actually grants the
	// repo.
	t.Run("github app with ssh clone url is created", func(t *testing.T) {
		userID := uuid.New()
		svc := newTestService(t, &fakeRepository{})
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath,
			strings.NewReader(body(SourceGitHubApp, "github", "acme/demo", "git@github.com:acme/demo.git"))))

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
	})

	// A linked create stores the connection id on the row; a foreign one is
	// not-found so connection ids cannot be probed.
	t.Run("github app link is owned", func(t *testing.T) {
		userID := uuid.New()
		owned := uuid.New()
		svc := newTestService(t, &fakeRepository{ownedGitHubApps: map[uuid.UUID]bool{owned: true}})
		srv := newRouteServer(svc, alwaysUser(userID))

		linked := map[string]any{
			"name": "demo app", "environment_id": uuid.New().String(),
			"provider": "github", "repo": "acme/demo",
			"clone_url": "https://github.com/acme/demo.git", "source_type": SourceGitHubApp,
			"branch": "main", "build_pack": "dockerfile", "server_id": uuid.New().String(),
			"github_app_id": owned.String(),
		}
		raw, err := json.Marshal(linked)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath, strings.NewReader(string(raw))))
		if rec.Code != http.StatusCreated {
			t.Fatalf("linked status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
		var envelope struct {
			Application struct {
				GitHubAppID string `json:"github_app_id"`
			} `json:"application"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Application.GitHubAppID != owned.String() {
			t.Errorf("link = %q, want %q", envelope.Application.GitHubAppID, owned.String())
		}

		linked["github_app_id"] = uuid.NewString()
		raw, err = json.Marshal(linked)
		if err != nil {
			t.Fatal(err)
		}
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath, strings.NewReader(string(raw))))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("foreign status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
		}
	})
}
