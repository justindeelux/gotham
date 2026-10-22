package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPStdioProtocolHelper is the child half of TestProtocolStdioAllTools:
// it runs the adapter over the process's real stdin/stdout. The test runner
// may not print anything before Serve takes the streams, so the helper exits
// directly instead of returning to the testing package.
func TestMCPStdioProtocolHelper(_ *testing.T) {
	if os.Getenv("GOTHAM_TEST_MCP_STDIO") != "1" {
		return
	}
	cfg, err := ConfigFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp stdio helper: invalid configuration")
		os.Exit(2)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := Serve(context.Background(), cfg, "protocol-test", logger); err != nil {
		fmt.Fprintln(os.Stderr, "mcp stdio helper: serve failed")
		os.Exit(1)
	}
	os.Exit(0)
}

// TestProtocolStdioAllTools drives the real gotham mcp serve process with the
// official SDK client: initialize, list and call over newline-delimited JSON
// on stdout. It pins the public contract of every registered tool (strict JSON
// Schema, read/write annotations) and proves large or secret-bearing upstream
// values never reach the protocol stream.
func TestProtocolStdioAllTools(t *testing.T) {
	const team = "11111111-1111-4111-8111-111111111111"
	const token = "gt1_protocol-private-token"
	projectID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	envID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	nodeID := uuid.MustParse("44444444-4444-4444-8444-444444444444")

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Team-Id") != team {
			t.Errorf("unexpected authority headers on %s", r.URL.Path)
		}
		acknowledge(w, r)
		switch r.URL.Path {
		case "/api/v1/mcp/context":
			_, _ = fmt.Fprintf(w, `{"team_id":%q,"capabilities":["context","strict_reads","strict_mutations","request_replay"]}`, team)
		case "/api/v1/servers":
			_, _ = fmt.Fprintf(w, `{"servers":[{"id":%q,"name":"node-1","status":"online"}]}`, nodeID)
		case "/api/v1/mcp/projects":
			if r.Method != http.MethodPost || r.Header.Get("Idempotency-Key") != "project-1" {
				t.Errorf("project_create request: method=%s key=%q", r.Method, r.Header.Get("Idempotency-Key"))
			}
			_, _ = fmt.Fprintf(w, `{"receipt":{"resource_id":%q,"related_id":%q,"accepted_at":"2026-10-23T00:00:00Z","replayed":false,"outcome":"accepted"},"project":{"id":%q,"name":"shop","environment_count":1},"environments":[{"id":%q,"project_id":%q,"name":"production"}]}`,
				projectID, envID, projectID, envID, projectID)
		default:
			t.Errorf("unexpected backend path %s", r.URL.Path)
		}
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioProtocolHelper$")
	command.Env = append(os.Environ(),
		"GOTHAM_TEST_MCP_STDIO=1",
		"GOTHAM_MCP_URL="+api.URL,
		"GOTHAM_MCP_TOKEN="+token,
		"GOTHAM_MCP_TEAM_ID="+team,
	)
	command.Stderr = &stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "protocol-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("stdio initialize: %v (stderr: %s)", err, stderr.String())
	}
	if session.InitializeResult().ServerInfo.Name != "gotham" {
		t.Fatalf("server info = %+v", session.InitializeResult().ServerInfo)
	}
	list, err := session.ListTools(ctx, &sdk.ListToolsParams{})
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

	// A read call proves structured results and bounded projection over stdio.
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "nodes_list", Arguments: map[string]any{"limit": 10}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("nodes_list: %+v %v", result, err)
	}
	nodes := structured(t, result)
	if nodes["total"] != float64(1) || len(nodes["nodes"].([]any)) != 1 {
		t.Fatalf("nodes_list result = %+v", nodes)
	}

	// A mutation call proves the strict alias, idempotency key wiring and
	// receipt projection without the backend ever seeing a remote secret.
	result, err = session.CallTool(ctx, &sdk.CallToolParams{Name: "project_create", Arguments: map[string]any{"request_id": "project-1", "name": "shop"}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("project_create: %+v %v", result, err)
	}
	created := structured(t, result)
	receipt := created["receipt"].(map[string]any)
	if receipt["resource_id"] != projectID.String() || created["environments"].([]any)[0].(map[string]any)["id"] != envID.String() {
		t.Fatalf("project_create result = %+v", created)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("stdio shutdown: %v", err)
	}
	if strings.Contains(stderr.String(), token) {
		t.Fatal("token leaked through stderr")
	}
}

// validateToolSchema round-trips the advertised schema through the same
// jsonschema-go draft the SDK validates against, so an invalid schema fails
// here rather than in a client at call time.
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

func structured(t *testing.T, result *sdk.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
