package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	protocolToken = "gt1_protocol-private-token"
)

var (
	protocolProjectID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	protocolEnvID     = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	protocolNodeID    = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	protocolAppID     = uuid.MustParse("55555555-5555-4555-8555-555555555555")
	protocolDeployID  = uuid.MustParse("66666666-6666-4666-8666-666666666666")
)

func TestProtocolHTTPAllTools(t *testing.T) {
	backend := &protocolBackend{mode: protocolNormal}
	api := protocolAPI(t, backend)
	session := connectHTTP(t, api.URL, protocolToken)
	if session.InitializeResult().ServerInfo.Name != "gotham" {
		t.Fatalf("server info = %+v", session.InitializeResult().ServerInfo)
	}
	list, err := session.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	wantRead := map[string]bool{
		"system_overview": true, "nodes_list": true, "node_overview": true, "node_metrics": true,
		"projects_list": true, "environments_list": true, "applications_list": true,
		"environment_resources": true, "project_overview": true, "application_overview": true,
		"application_logs": true, "deployment_logs": true,
		"deployments_list": true, "deployment_status": true,
	}
	wantWrite := map[string]bool{
		"project_create": true, "environment_create": true, "application_create": true,
		"application_deploy": true,
	}
	if len(list.Tools) != len(wantRead)+len(wantWrite) {
		t.Fatalf("tools=%d want=%d", len(list.Tools), len(wantRead)+len(wantWrite))
	}
	seen := map[string]bool{}
	for _, tool := range list.Tools {
		seen[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("%s: empty description", tool.Name)
		}
		validateToolSchema(t, tool)
		annotation := tool.Annotations
		if annotation == nil {
			t.Errorf("%s: missing annotations", tool.Name)
			continue
		}
		switch {
		case wantRead[tool.Name]:
			if !annotation.ReadOnlyHint || !annotation.IdempotentHint {
				t.Errorf("%s: read tool annotations = %+v", tool.Name, annotation)
			}
		case wantWrite[tool.Name]:
			if annotation.ReadOnlyHint || !annotation.IdempotentHint {
				t.Errorf("%s: write tool annotations = %+v", tool.Name, annotation)
			}
			if (tool.Name == "application_deploy") != (annotation.DestructiveHint != nil && *annotation.DestructiveHint) {
				t.Errorf("%s: destructive hint = %v", tool.Name, annotation.DestructiveHint)
			}
		default:
			t.Errorf("%s: not in the documented tool set", tool.Name)
		}
	}
	for name := range wantRead {
		if !seen[name] {
			t.Errorf("missing read tool %s", name)
		}
	}
	for name := range wantWrite {
		if !seen[name] {
			t.Errorf("missing write tool %s", name)
		}
	}

	calls := []struct {
		name string
		args map[string]any
	}{
		{"system_overview", map[string]any{}},
		{"nodes_list", map[string]any{"limit": 10}},
		{"node_overview", map[string]any{"node_id": protocolNodeID.String()}},
		{"node_metrics", map[string]any{"node_id": protocolNodeID.String(), "from": "2026-10-23T00:00:00Z", "to": "2026-10-23T01:00:00Z", "step": "1m"}},
		{"projects_list", map[string]any{}},
		{"environments_list", map[string]any{"project_id": protocolProjectID.String()}},
		{"applications_list", map[string]any{}},
		{"environment_resources", map[string]any{"environment_id": protocolEnvID.String()}},
		{"project_overview", map[string]any{"project_id": protocolProjectID.String()}},
		{"application_overview", map[string]any{"application_id": protocolAppID.String()}},
		{"application_logs", map[string]any{"application_id": protocolAppID.String()}},
		{"deployment_logs", map[string]any{"application_id": protocolAppID.String(), "deployment_id": protocolDeployID.String()}},
		{"deployments_list", map[string]any{"application_id": protocolAppID.String()}},
		{"deployment_status", map[string]any{"application_id": protocolAppID.String(), "deployment_id": protocolDeployID.String()}},
		{"project_create", map[string]any{"request_id": "protocol-project", "name": "shop"}},
		{"environment_create", map[string]any{"request_id": "protocol-env", "project_id": protocolProjectID.String(), "name": "staging"}},
		{"application_create", map[string]any{"request_id": "protocol-app", "name": "app", "environment_id": protocolEnvID.String(), "server_id": protocolNodeID.String(), "source_type": "image", "image_ref": "busybox:1.36"}},
		{"application_deploy", map[string]any{"application_id": protocolAppID.String(), "request_id": "protocol-deploy"}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil || result.IsError || result.StructuredContent == nil {
				t.Fatalf("%s: %+v %v", tc.name, result, err)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), protocolToken) {
				t.Fatalf("%s projected the fixed token", tc.name)
			}
		})
	}
	if err := session.Close(); err != nil {
		t.Fatalf("HTTP shutdown: %v", err)
	}
}

func TestProtocolHTTPTokenFailures(t *testing.T) {
	for _, tc := range []struct{ name, token string }{
		{"invalid gt1", "gt1_invalid"},
		{"revoked gt1", "gt1_revoked"},
		{"JWT rejected", "eyJhbGciOi.secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &protocolBackend{mode: protocolNormal}
			api := protocolAPI(t, backend)
			if _, err := connectHTTPErr(api.URL, tc.token); err == nil {
				t.Fatal("endpoint accepted a rejected credential")
			} else if strings.Contains(err.Error(), tc.token) {
				t.Fatal("credential leaked through the transport error")
			}
		})
	}

	t.Run("old backend", func(t *testing.T) {
		backend := &protocolBackend{mode: protocolOldBackend}
		api := protocolAPI(t, backend)
		session := connectHTTP(t, api.URL, protocolToken)
		defer session.Close()
		for _, name := range []string{"nodes_list", "project_create"} {
			args := map[string]any{}
			if name == "project_create" {
				args = map[string]any{"request_id": "old", "name": "shop"}
			}
			result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
			if err != nil || !result.IsError {
				t.Fatalf("%s against an old backend: %+v %v", name, result, err)
			}
			text, ok := result.Content[0].(*sdk.TextContent)
			if !ok || text.Text != "unsupported_or_not_found" {
				t.Fatalf("%s error=%+v", name, result.Content)
			}
		}
		if writes := backend.writeCount(); writes != 0 {
			t.Fatalf("old backend received %d legacy mutation attempt(s)", writes)
		}
	})

	t.Run("legacy write counter moves on a POST", func(t *testing.T) {
		backend := &protocolBackend{mode: protocolOldBackend}
		api := httptest.NewServer(backend.handler(t))
		defer api.Close()
		response, err := http.Post(api.URL+"/api/v1/projects", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound || backend.writeCount() != 1 {
			t.Fatalf("counter did not move: status=%d writes=%d", response.StatusCode, backend.writeCount())
		}
	})
}

type protocolMode int

const (
	protocolNormal protocolMode = iota
	protocolOldBackend
)

type protocolBackend struct {
	mode         protocolMode
	mu           sync.Mutex
	legacyWrites int
}

func (b *protocolBackend) writeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.legacyWrites
}

func (b *protocolBackend) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasPrefix(r.URL.Path, "/api/v1/mcp/") {
			b.mu.Lock()
			b.legacyWrites++
			b.mu.Unlock()
		}
		switch b.mode {
		case protocolOldBackend:
			if r.URL.Path != "/api/v1/mcp/context" {
				http.NotFound(w, r)
				return
			}
		default:
			if r.URL.Path != "/healthz" && (r.Header.Get("Authorization") != "Bearer "+protocolToken) {
				t.Errorf("unexpected authority headers on %s", r.URL.Path)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if r.URL.Path != "/healthz" {
			w.Header().Set(contractHeader, "1")
		}
		protocolRoute(t, w, r)
	}
}

func protocolRoute(t *testing.T, w http.ResponseWriter, r *http.Request) {
	writeJSON := func(status int, payload string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, payload)
	}
	now := "2026-10-23T00:00:00Z"
	receipt := func(resource, related string) string {
		return fmt.Sprintf(`{"resource_id":%q,"related_id":%q,"accepted_at":%q,"replayed":false,"outcome":"accepted"}`, resource, related, now)
	}
	project := fmt.Sprintf(`{"id":%q,"name":"shop","environment_count":1,"resource_counts":{}}`, protocolProjectID)
	environment := fmt.Sprintf(`{"id":%q,"project_id":%q,"name":"production","resource_counts":{}}`, protocolEnvID, protocolProjectID)
	node := fmt.Sprintf(`{"id":%q,"name":"node-1","status":"online"}`, protocolNodeID)
	app := fmt.Sprintf(`{"id":%q,"name":"app","project_id":%q,"environment_id":%q,"server_id":%q,"source_type":"image"}`, protocolAppID, protocolProjectID, protocolEnvID, protocolNodeID)
	deployment := fmt.Sprintf(`{"id":%q,"application_id":%q,"kind":"deploy","state":"running","attempt":1,"created_at":%q,"updated_at":%q}`, protocolDeployID, protocolAppID, now, now)

	path := r.URL.Path
	switch {
	case path == "/healthz":
		writeJSON(http.StatusOK, `{"status":"ok","db":"ok","redis":"ok"}`)
	case path == "/api/v1/mcp/context":
		writeJSON(http.StatusOK, `{"capabilities":["context","strict_reads","strict_mutations","request_replay"]}`)
	case path == "/api/v1/version":
		writeJSON(http.StatusOK, `{"version":"0.3.0"}`)
	case path == "/api/v1/servers":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"servers":[%s]}`, node))
	case path == "/api/v1/servers/"+protocolNodeID.String():
		writeJSON(http.StatusOK, fmt.Sprintf(`{"server":%s}`, node))
	case path == "/api/v1/servers/"+protocolNodeID.String()+"/metrics":
		writeJSON(http.StatusOK, `{"step":"1m","points":[]}`)
	case path == "/api/v1/projects":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"projects":[%s]}`, project))
	case path == "/api/v1/projects/"+protocolProjectID.String():
		writeJSON(http.StatusOK, fmt.Sprintf(`{"project":%s,"environments":[%s]}`, project, environment))
	case path == "/api/v1/projects/"+protocolProjectID.String()+"/environments":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"environments":[%s]}`, environment))
	case path == "/api/v1/environments/"+protocolEnvID.String()+"/resources":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"environment":%s,"project":%s,"applications":[%s],"services":[],"databases":[]}`, environment, project, app))
	case path == "/api/v1/applications":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"applications":[%s]}`, app))
	case path == "/api/v1/applications/"+protocolAppID.String():
		writeJSON(http.StatusOK, fmt.Sprintf(`{"application":%s}`, app))
	case path == "/api/v1/applications/"+protocolAppID.String()+"/deployments":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"deployments":[%s]}`, deployment))
	case path == "/api/v1/applications/"+protocolAppID.String()+"/deployments/"+protocolDeployID.String():
		writeJSON(http.StatusOK, fmt.Sprintf(`{"deployment":%s}`, deployment))
	case path == "/api/v1/applications/"+protocolAppID.String()+"/deployments/"+protocolDeployID.String()+"/logs":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"application_id":%q,"deployment_id":%q,"source":"persisted_build","availability":"available","prior_truncation":"unknown","log":"built %s","complete":true}`, protocolAppID, protocolDeployID, protocolToken))
	case path == "/api/v1/applications/"+protocolAppID.String()+"/runtime":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"application_id":%q,"observed_at":%q,"coverage":"full","containers":[]}`, protocolAppID, now))
	case path == "/api/v1/applications/"+protocolAppID.String()+"/logs":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"application_id":%q,"source":"runtime","containers":[{"id":"%s","state":"running","health":"unknown","log":"runtime %s"}],"selection_truncated":false,"complete":true,"partial":false}`, protocolAppID, strings.Repeat("a", 64), protocolToken))
	case path == "/api/v1/deployments":
		writeJSON(http.StatusOK, fmt.Sprintf(`{"deployments":[%s]}`, deployment))
	case path == "/api/v1/mcp/projects" && r.Method == http.MethodPost:
		writeJSON(http.StatusCreated, fmt.Sprintf(`{"receipt":%s,"project":%s,"environments":[%s]}`, receipt(protocolProjectID.String(), protocolEnvID.String()), project, environment))
	case path == "/api/v1/mcp/projects/"+protocolProjectID.String()+"/environments" && r.Method == http.MethodPost:
		writeJSON(http.StatusCreated, fmt.Sprintf(`{"receipt":%s,"environment":%s}`, receipt(protocolEnvID.String(), ""), environment))
	case path == "/api/v1/mcp/applications" && r.Method == http.MethodPost:
		writeJSON(http.StatusCreated, fmt.Sprintf(`{"receipt":%s,"application":{%s,"environment_name":"production"},"webhook_outcome":"none"}`, receipt(protocolAppID.String(), protocolProjectID.String()), strings.Trim(app, "{}")))
	case path == "/api/v1/mcp/applications/"+protocolAppID.String()+"/deploy" && r.Method == http.MethodPost:
		writeJSON(http.StatusAccepted, fmt.Sprintf(`{"receipt":%s,"deployment":%s}`, receipt(protocolDeployID.String(), ""), deployment))
	default:
		t.Errorf("unexpected backend path %s %s", r.Method, path)
		http.NotFound(w, r)
	}
}

func validateToolSchema(t *testing.T, tool *sdk.Tool) {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("%s: schema marshal: %v", tool.Name, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("%s: schema decode: %v", tool.Name, err)
	}
	if _, err := schema.Resolve(nil); err != nil {
		t.Fatalf("%s: schema resolve: %v", tool.Name, err)
	}
	if schema.Type != "object" {
		t.Fatalf("%s: schema type=%q", tool.Name, schema.Type)
	}
}

// protocolAPI exposes the REST backend and the streamable HTTP MCP endpoint at
// /mcp. The endpoint authenticates the bearer token before building the tool
// surface, like the control plane, so a rejected credential fails on connect.
func protocolAPI(t *testing.T, backend *protocolBackend) *httptest.Server {
	t.Helper()
	var api *httptest.Server
	streamable := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		client, err := NewClient(Config{URL: api.URL, Token: protocolToken})
		if err != nil {
			return nil
		}
		return NewServerWithClient(client, true, "protocol-test", nil)
	}, &sdk.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        MaxBody,
	})
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			backend.handler(t).ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+protocolToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		streamable.ServeHTTP(w, r)
	}))
	t.Cleanup(api.Close)
	return api
}

func connectHTTP(t *testing.T, apiURL, token string) *sdk.ClientSession {
	t.Helper()
	session, err := connectHTTPErr(apiURL, token)
	if err != nil {
		t.Fatalf("HTTP connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func connectHTTPErr(apiURL, token string) (*sdk.ClientSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	client := sdk.NewClient(&sdk.Implementation{Name: "protocol-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             apiURL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	cancel()
	return session, err
}

type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
}
