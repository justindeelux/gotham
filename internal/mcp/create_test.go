package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func createSession(t *testing.T, handler http.HandlerFunc) *sdk.ClientSession {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer gt1_test-secret" || r.Header.Get(contractHeader) != "1" || r.Header.Get("Idempotency-Key") != "stable-1" {
			t.Errorf("incorrect mutation request: %s %s", r.Method, r.URL.Path)
		}
		acknowledge(w, r)
		handler(w, r)
	}))
	t.Cleanup(api.Close)
	client, err := NewClient(testConfig(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	registerCreateTools(server, client)
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func createArgs(source string) map[string]any {
	args := map[string]any{"request_id": "stable-1", "name": "app", "environment_id": uuid.NewString(), "server_id": uuid.NewString(), "source_type": source}
	switch source {
	case "git_public", "git_private":
		args["clone_url"] = "https://example.com/repo.git"
	case "github_app", "gitlab_app":
		args["provider"] = strings.TrimSuffix(source, "_app")
		args["repo"] = "owner/repo"
		args["clone_url"] = "https://example.com/repo.git"
	case "dockerfile":
		args["dockerfile_content"] = "FROM busybox\nCMD [\"true\"]"
	case "compose":
		args["compose_content"] = "services:\n  web:\n    image: nginx\n"
		args["compose_service"] = "web"
	case "image":
		args["image_ref"] = "nginx:alpine"
	}
	return args
}
func TestCreateSourcesAndSafeProjection(t *testing.T) {
	calls := 0
	appID := uuid.NewString()
	cs := createSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/mcp/applications" {
			t.Errorf("legacy or deploy path %s", r.URL.Path)
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if _, ok := input["request_id"]; ok {
			t.Error("identity leaked into payload")
		}
		if source := input["source_type"]; source == "git_public" || source == "image" {
			if input["branch"] != nil {
				t.Error("guessed branch")
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"receipt": map[string]any{"resource_id": appID, "replayed": calls > 1, "outcome": "accepted", "webhook_outcome": "warning"}, "application": map[string]any{"id": appID, "name": "app", "source_type": input["source_type"], "environment_name": "production", "environment_id": input["environment_id"], "server_id": input["server_id"], "registry_password": "secret-sentinel", "build_args": map[string]string{"TOKEN": "secret-sentinel"}}, "webhook": map[string]any{"installed": false, "error": "secret-sentinel"}})
	})
	for _, source := range []string{"git_public", "git_private", "github_app", "gitlab_app", "dockerfile", "compose", "image"} {
		t.Run(source, func(t *testing.T) {
			result := callDiscovery(t, cs, "application_create", createArgs(source))
			data := resultJSON(t, result)
			if result.IsError || !strings.Contains(data, appID) || !strings.Contains(data, "warning") || strings.Contains(data, "secret-sentinel") {
				t.Fatalf("bad create %+v %s", result, data)
			}
		})
	}
	args := createArgs("compose")
	delete(args, "compose_content")
	args["compose_file"] = "compose.yml"
	args["clone_url"] = "https://example.com/repo.git"
	if result := callDiscovery(t, cs, "application_create", args); result.IsError {
		t.Fatalf("repo compose %+v", result)
	}
}
func TestCreateSourceSchemasRejectConflicts(t *testing.T) {
	calls := 0
	cs := createSession(t, func(http.ResponseWriter, *http.Request) { calls++ })
	cases := []struct {
		source, key string
		value       any
	}{
		{"image", "branch", "main"}, {"image", "repo", "owner/repo"}, {"image", "build_pack", "dockerfile"}, {"image", "provider", "github"},
		{"dockerfile", "clone_url", "https://example.com/repo.git"}, {"compose", "provider", "github"}, {"compose", "compose_file", "compose.yml"}, {"compose", "branch", "main"},
		{"git_public", "image_ref", "nginx"}, {"git_private", "provider", "gitlab"}, {"github_app", "provider", "gitlab"}, {"gitlab_app", "github_app_id", uuid.NewString()},
		{"git_public", "source_type", "invalid"}, {"git_public", "environment_id", "foreign-name"}, {"image", "server_id", uuid.Nil.String()},
		{"image", "request_id", "bad key"}, {"image", "url", "https://attacker.example"},
	}
	for _, tt := range cases {
		t.Run(tt.source+"/"+tt.key, func(t *testing.T) {
			args := createArgs(tt.source)
			args[tt.key] = tt.value
			result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "application_create", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			assertCreateError(t, result, "invalid_request")
			if calls != 0 {
				t.Fatalf("invalid %s/%s reached backend", tt.source, tt.key)
			}
		})
	}
	for _, source := range []string{"git_public", "git_private", "github_app", "gitlab_app", "dockerfile", "compose", "image"} {
		args := createArgs(source)
		for _, key := range []string{"clone_url", "repo", "dockerfile_content", "compose_content", "image_ref"} {
			delete(args, key)
		}
		result, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "application_create", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		assertCreateError(t, result, "invalid_request")
		if calls != 0 {
			t.Fatalf("missing %s source reached backend", source)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid inputs wrote %d times", calls)
	}
}
func TestProjectAndEnvironmentCreateReturnedIdentity(t *testing.T) {
	project, environment := uuid.NewString(), uuid.NewString()
	cs := createSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/mcp/projects":
			_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"related_id":%q,"outcome":"accepted"},"project":{"id":%q,"name":"test"},"environments":[{"id":%q,"name":"actual-production"}]}`, project, environment, project, environment)
		case "/api/v1/mcp/projects/" + project + "/environments":
			_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"outcome":"accepted"},"environment":{"id":%q,"name":"staging"}}`, environment, environment)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	out := callDiscovery(t, cs, "project_create", map[string]any{"request_id": "stable-1", "name": "test"})
	if out.IsError || !strings.Contains(resultJSON(t, out), "actual-production") || strings.Contains(resultJSON(t, out), "webhook_outcome") {
		t.Fatalf("project %+v", out)
	}
	out = callDiscovery(t, cs, "environment_create", map[string]any{"request_id": "stable-1", "project_id": project, "name": "staging"})
	if out.IsError || !strings.Contains(resultJSON(t, out), environment) || strings.Contains(resultJSON(t, out), "webhook_outcome") {
		t.Fatalf("environment %+v", out)
	}
}
func TestCreateReplayFailuresAndAcceptedUnknown(t *testing.T) {
	for _, code := range []string{"request_conflict", "request_expired", "request_in_progress", "request_target_missing", ""} {
		t.Run(code, func(t *testing.T) {
			calls := 0
			cs := createSession(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(409)
				_, _ = fmt.Fprintf(w, `{"code":%q,"message":"secret-sentinel"}`, code)
			})
			out := callDiscovery(t, cs, "application_create", createArgs("image"))
			wantCode := code
			if wantCode == "" {
				wantCode = "upstream_error"
			}
			assertCreateError(t, out, wantCode)
			data, _ := json.Marshal(out)
			if !out.IsError || strings.Contains(string(data), "secret-sentinel") || calls != 1 {
				t.Fatalf("unsafe failure %s", data)
			}
		})
	}
	for _, hook := range []string{"not_confirmed", "", "warning", "installed"} {
		t.Run("accepted/"+hook, func(t *testing.T) {
			id := uuid.NewString()
			cs := createSession(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"outcome":"accepted_not_confirmed","replayed":true,"webhook_outcome":%q}}`, id, hook)
			})
			out := callDiscovery(t, cs, "application_create", createArgs("image"))
			if out.IsError || !strings.Contains(resultJSON(t, out), id) || !strings.Contains(resultJSON(t, out), "accepted_not_confirmed") {
				t.Fatalf("lost acceptance %+v", out)
			}
		})
	}
}
