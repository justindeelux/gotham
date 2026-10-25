package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func discoverySession(t *testing.T, handler http.HandlerFunc) *sdk.ClientSession {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gt1_test-secret" {
			t.Errorf("fixed authorization missing")
		}
		acknowledge(w, r)
		handler(w, r)
	}))
	t.Cleanup(api.Close)
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "test"}, nil)
	client, err := NewClient(testConfig(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	registerDiscoveryTools(server, client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func callDiscovery(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func resultJSON(t *testing.T, out *sdk.CallToolResult) string {
	t.Helper()
	data, err := json.Marshal(out.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func TestDiscoveryPagesAndSchemas(t *testing.T) {
	var requests atomic.Int32
	cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api/v1/projects":
			projects := make([]map[string]any, 105)
			for i := range projects {
				projects[i] = map[string]any{"id": uuid.NewString(), "name": "duplicate", "build_args": map[string]string{"password": "secret-sentinel"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": projects})
		default:
			t.Errorf("unexpected read %s", r.URL.String())
			w.WriteHeader(404)
		}
	})
	tools, err := cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil || len(tools.Tools) != 6 {
		t.Fatalf("tools %+v %v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("unsafe annotations: %+v", tool)
		}
		if tool.OutputSchema == nil {
			t.Fatalf("missing typed output %s", tool.Name)
		}
	}
	for _, test := range []struct {
		args         map[string]any
		count, total int
		truncated    bool
	}{{map[string]any{}, 20, 105, true}, {map[string]any{"limit": 100}, 100, 105, true}, {map[string]any{"limit": 100, "offset": 100}, 5, 105, false}, {map[string]any{"offset": 999}, 0, 105, false}} {
		out := callDiscovery(t, cs, "projects_list", test.args)
		if out.IsError {
			t.Fatalf("page error: %+v", out)
		}
		var page collectionPage[projectMetadata]
		if err = json.Unmarshal([]byte(resultJSON(t, out)), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != test.count || page.Total != test.total || page.Truncated != test.truncated || page.SnapshotConsistent {
			t.Fatalf("page=%+v", page)
		}
		if strings.Contains(resultJSON(t, out), "secret-sentinel") {
			t.Fatal("unprojected configuration leaked")
		}
	}
	for _, args := range []map[string]any{{"limit": 101}, {"offset": -1}, {"owner_id": uuid.NewString()}, {"url": "http://override"}, {"token": "override"}} {
		out := callDiscovery(t, cs, "projects_list", args)
		if !out.IsError {
			t.Fatalf("invalid input accepted: %v", args)
		}
	}
	before := requests.Load()
	for _, args := range []map[string]any{{"project_id": uuid.NewString(), "environment_id": uuid.NewString()}, {"project_id": "name"}, {"environment_id": uuid.Nil.String()}} {
		if !callDiscovery(t, cs, "applications_list", args).IsError {
			t.Fatalf("invalid selector accepted %v", args)
		}
	}
	if requests.Load() != before {
		t.Fatal("invalid selectors reached API")
	}
}
func TestDiscoveryEmptyAndForeignIDs(t *testing.T) {
	cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects" {
			_, _ = fmt.Fprint(w, `{"projects":[]}`)
			return
		}
		w.WriteHeader(404)
		_, _ = fmt.Fprint(w, `{"source":"secret-sentinel"}`)
	})
	out := callDiscovery(t, cs, "projects_list", map[string]any{})
	if out.IsError || !strings.Contains(resultJSON(t, out), `"items":[]`) {
		t.Fatalf("empty %+v", out)
	}
	for name, args := range map[string]map[string]any{"project_overview": {"project_id": uuid.NewString()}, "environments_list": {"project_id": uuid.NewString()}, "environment_resources": {"environment_id": uuid.NewString()}, "application_overview": {"application_id": uuid.NewString()}} {
		out = callDiscovery(t, cs, name, args)
		data, _ := json.Marshal(out)
		if !out.IsError || strings.Contains(string(data), "secret-sentinel") {
			t.Fatalf("foreign/missing response leaked: %s", data)
		}
	}
}
func TestDiscoveryApplicationTruthAndProjection(t *testing.T) {
	app := uuid.NewString()
	cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications/" + app:
			_, _ = fmt.Fprintf(w, `{"application":{"id":%q,"name":"demo","build_args":{"TOKEN":"secret-sentinel"},"compose_content":"secret-sentinel","dockerfile_content":"secret-sentinel","clone_url":"https://secret-sentinel@example.com"}}`, app)
		case "/api/v1/applications/" + app + "/deployments":
			if r.URL.Query().Get("limit") != "5" {
				t.Error("unbounded deployment history")
			}
			_, _ = fmt.Fprintf(w, `{"deployments":[{"id":%q,"application_id":%q,"state":"running","error":"secret-sentinel","build_log":"secret-sentinel"}]}`, uuid.NewString(), app)
		case "/api/v1/applications/" + app + "/runtime":
			_, _ = fmt.Fprintf(w, `{"application_id":%q,"coverage":"complete","observed_at":"2026-10-22T00:00:00Z","containers":[{"id":%q,"state":"exited","health":"unknown","deployment_id":null,"labels":{"secret":"secret-sentinel"},"mounts":["secret-sentinel"]}]}`, app, strings.Repeat("a", 64))
		default:
			t.Fatalf("unexpected API %s", r.URL.String())
		}
	})
	out := callDiscovery(t, cs, "application_overview", map[string]any{"application_id": app})
	data := resultJSON(t, out)
	if out.IsError || strings.Contains(data, "secret-sentinel") {
		t.Fatalf("unsafe output %+v %s", out, data)
	}
	var overview applicationOverview
	_ = json.Unmarshal([]byte(data), &overview)
	if overview.Pipeline[0].State != "running" || overview.Runtime.Containers[0].State != "exited" || overview.Runtime.Containers[0].Health != "unknown" || overview.Runtime.Containers[0].DeploymentID != nil {
		t.Fatalf("invented readiness %+v", overview)
	}
}
func TestDiscoveryPartialAndRevocation(t *testing.T) {
	for _, status := range []int{404, 502, 401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			app := uuid.NewString()
			cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/applications/"+app {
					_, _ = fmt.Fprintf(w, `{"application":{"id":%q}}`, app)
					return
				}
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "secret-sentinel")
			})
			out := callDiscovery(t, cs, "application_overview", map[string]any{"application_id": app})
			if out.IsError != (status == 401 || status == 403) {
				t.Fatalf("status %d result %+v", status, out)
			}
			if !out.IsError {
				var overview applicationOverview
				_ = json.Unmarshal([]byte(resultJSON(t, out)), &overview)
				if overview.Runtime != nil || overview.RuntimeError == nil || overview.PipelineError == nil {
					t.Fatalf("unavailable became empty observation %+v", overview)
				}
			}
			data, _ := json.Marshal(out)
			if strings.Contains(string(data), "secret-sentinel") {
				t.Fatal("upstream error leaked")
			}
		})
	}
}
func TestDiscoveryProjectBoundedPartialInventory(t *testing.T) {
	project, app := uuid.NewString(), uuid.NewString()
	envs := make([]environmentMetadata, 8)
	for i := range envs {
		envs[i] = environmentMetadata{ID: uuid.NewString(), ProjectID: project, Name: "duplicate"}
	}
	var inventories atomic.Int32
	cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/" + project:
			_ = json.NewEncoder(w).Encode(map[string]any{"project": projectMetadata{ID: project, Name: "demo", ResourceCounts: resourceCounts{Applications: 15}}, "environments": envs})
		case "/api/v1/applications":
			if r.URL.Query().Get("project_id") != project {
				t.Error("project filter missing")
			}
			_, _ = fmt.Fprintf(w, `{"applications":[{"id":%q,"project_id":%q}]}`, app, project)
		case "/api/v1/deployments":
			if r.URL.Query().Get("limit") != "100" {
				t.Error("activity unbounded")
			}
			_, _ = fmt.Fprintf(w, `{"deployments":[{"id":%q,"application_id":%q,"state":"running"},{"id":%q,"application_id":%q,"state":"running"}]}`, uuid.NewString(), app, uuid.NewString(), uuid.NewString())
		default:
			if !strings.HasPrefix(r.URL.Path, "/api/v1/environments/") || !strings.HasSuffix(r.URL.Path, "/resources") {
				t.Errorf("unexpected API %s", r.URL.Path)
			}
			inventories.Add(1)
			w.WriteHeader(502)
		}
	})
	out := callDiscovery(t, cs, "project_overview", map[string]any{"project_id": project})
	var overview projectOverview
	_ = json.Unmarshal([]byte(resultJSON(t, out)), &overview)
	if out.IsError || inventories.Load() != 4 || !overview.InventoryTruncated || len(overview.Inventory) != 4 || len(overview.RecentActivity) != 1 || overview.LiveRuntimeInspected || overview.Project.ResourceCounts.Applications != 15 {
		t.Fatalf("overview=%+v result=%+v", overview, out)
	}
	for _, section := range overview.Inventory {
		if section.Inventory != nil || section.Error == nil {
			t.Fatalf("failed section reported fictional zero inventory %+v", section)
		}
	}
}
func TestDiscoveryEnvironmentResourcesSafeAndBounded(t *testing.T) {
	env := uuid.NewString()
	project := uuid.NewString()
	cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects/"+project+"/environments" {
			_, _ = fmt.Fprintf(w, `{"environments":[{"id":%q,"project_id":%q,"name":"production"}]}`, env, project)
			return
		}
		apps := make([]map[string]any, 110)
		for i := range apps {
			apps[i] = map[string]any{"id": uuid.NewString(), "name": "duplicate", "clone_url": "secret-sentinel"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"environment": environmentMetadata{ID: env}, "project": projectMetadata{ID: project}, "applications": apps, "services": []map[string]any{{"id": uuid.NewString(), "name": "cache", "env": "secret-sentinel", "compose_content": "secret-sentinel"}}, "databases": []map[string]any{{"id": uuid.NewString(), "password": "secret-sentinel"}}})
	})
	out := callDiscovery(t, cs, "environment_resources", map[string]any{"environment_id": env})
	data := resultJSON(t, out)
	if out.IsError || strings.Contains(data, "secret-sentinel") {
		t.Fatalf("unsafe inventory %s", data)
	}
	var inventory environmentInventory
	_ = json.Unmarshal([]byte(data), &inventory)
	if len(inventory.Applications.Items) != 100 || !inventory.Applications.Truncated || inventory.Applications.Total != 110 {
		t.Fatalf("unbounded inventory %+v", inventory)
	}
	out = callDiscovery(t, cs, "environments_list", map[string]any{"project_id": project})
	if out.IsError || !strings.Contains(resultJSON(t, out), "production") {
		t.Fatalf("environment listing %+v", out)
	}
}
