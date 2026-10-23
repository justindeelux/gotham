package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	protocolTeam  = "11111111-1111-4111-8111-111111111111"
	protocolToken = "gt1_protocol-private-token"
)

var (
	protocolProjectID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	protocolEnvID     = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	protocolNodeID    = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	protocolAppID     = uuid.MustParse("55555555-5555-4555-8555-555555555555")
	protocolDeployID  = uuid.MustParse("66666666-6666-4666-8666-666666666666")
)

// TestMCPStdioProtocolHelper is the child half of the protocol tests: it runs
// the adapter over the process's real stdin/stdout. The test runner must not
// print anything before Serve takes the streams, so the helper exits directly
// instead of returning to the testing package.
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
		code := "serve_failed"
		var safe *Error
		if errors.As(err, &safe) {
			code = safe.Code
		}
		fmt.Fprintf(os.Stderr, "mcp stdio helper: code=%s\n", code)
		os.Exit(1)
	}
	os.Exit(0)
}

// TestProtocolStdioAllTools drives the real gomcp serve process with the
// official SDK client: initialize, list and call every one of the 18 tools over
// newline-delimited JSON on stdout. It pins the public contract of each tool
// (strict JSON Schema, read/write annotations, structured result) and proves
// the fixed token is never projected through the protocol or stderr.
func TestProtocolStdioAllTools(t *testing.T) {
	backend := &protocolBackend{mode: protocolNormal}
	api := httptest.NewServer(backend.handler(t))
	defer api.Close()
	session, stderr, err := connectStdio(t, api.URL, protocolToken)
	if err != nil {
		t.Fatalf("stdio initialize: %v (stderr: %s)", err, stderr)
	}
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

	// Every tool gets one minimal happy-path call. The backend serves one
	// stable dataset, so the table proves registration, schema acceptance and
	// structured result decoding for all 18 rather than re-testing each tool's
	// projection rules (covered by the dedicated unit suites).
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
		t.Fatalf("stdio shutdown: %v", err)
	}
	if strings.Contains(stderr.String(), protocolToken) {
		t.Fatal("token leaked through stderr")
	}
}

// TestProtocolStdioStdoutTranscript sends raw frames and parses the real
// stdout transcript: every line must be a JSON-RPC frame and the fixed token
// must never appear.
func TestProtocolStdioStdoutTranscript(t *testing.T) {
	backend := &protocolBackend{mode: protocolNormal}
	api := httptest.NewServer(backend.handler(t))
	defer api.Close()
	stdout, stderr, err := runStdioRaw(t, api.URL, protocolToken, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"raw-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nodes_list","arguments":{"limit":10}}}`,
	})
	if err != nil {
		t.Fatalf("raw stdio: %v (stderr: %s)", err, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("stdout frames=%d: %s", len(lines), stdout)
	}
	for _, line := range lines {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil || frame["jsonrpc"] != "2.0" {
			t.Fatalf("stdout corruption: %q", line)
		}
	}
	for _, text := range []string{stdout, stderr} {
		if strings.Contains(text, protocolToken) {
			t.Fatal("token leaked through raw stdio")
		}
	}
}

// TestProtocolStdioTokenFailures covers the startup failures a client sees over
// real stdio: an invalid gt1_ token, a revoked token (backend 401), a JWT that
// is not an API token, and an old backend whose context works but whose strict
// reads/mutations are 404 with no legacy write fallback.
func TestProtocolStdioTokenFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  protocolMode
		token string
		code  string
		exit  int
	}{
		{"invalid gt1", protocolUnauthorized, "gt1_invalid", "unauthorized", 1},
		{"revoked gt1", protocolUnauthorized, "gt1_revoked", "unauthorized", 1},
		{"JWT rejected", protocolNormal, "eyJhbGciOi.secret", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &protocolBackend{mode: tc.mode}
			api := httptest.NewServer(backend.handler(t))
			defer api.Close()
			_, stderr, err := connectStdio(t, api.URL, tc.token)
			if err == nil {
				t.Fatal("adapter initialized with a rejected credential")
			}
			if strings.Contains(stderr.String(), tc.token) {
				t.Fatal("credential leaked through stderr")
			}
			if tc.code != "" && !strings.Contains(stderr.String(), "code="+tc.code) {
				t.Fatalf("stderr=%q want code=%s", stderr, tc.code)
			}
			if tc.code == "" && !strings.Contains(stderr.String(), "invalid configuration") {
				t.Fatalf("stderr=%q want local configuration error", stderr)
			}
		})
	}

	t.Run("old backend", func(t *testing.T) {
		backend := &protocolBackend{mode: protocolOldBackend}
		api := httptest.NewServer(backend.handler(t))
		defer api.Close()
		session, _, err := connectStdio(t, api.URL, protocolToken)
		if err != nil {
			t.Fatalf("context should still initialize: %v", err)
		}
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

	// Negative control: the same counter must move for a POST to a legacy
	// alias, proving the old-backend zero above is meaningful.
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
	protocolUnauthorized
	protocolOldBackend
)

// protocolBackend is one stable fake control plane for every tool shape. It
// records successful mutations so an old-backend fallback cannot hide.
type protocolBackend struct {
	mode         protocolMode
	mu           sync.Mutex
	legacyWrites int
}

// writeCount counts non-GET requests to a non-strict alias. A correct adapter
// only mutates through /api/v1/mcp/*; a legacy fallback would move this
// counter even when the old backend answers 404.
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
		case protocolUnauthorized:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case protocolOldBackend:
			if r.URL.Path != "/api/v1/mcp/context" {
				http.NotFound(w, r)
				return
			}
		default:
			if r.URL.Path != "/healthz" && (r.Header.Get("Authorization") != "Bearer "+protocolToken || r.Header.Get("X-Team-Id") != protocolTeam) {
				t.Errorf("unexpected authority headers on %s", r.URL.Path)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if r.URL.Path != "/healthz" {
			w.Header().Set(contractHeader, "1")
			w.Header().Set(teamHeader, protocolTeam)
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
		writeJSON(http.StatusOK, fmt.Sprintf(`{"team_id":%q,"capabilities":["context","strict_reads","strict_mutations","request_replay"]}`, protocolTeam))
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

// connectStdio starts the real helper process and connects the SDK client. The
// child is closed and reaped when the test ends.
func connectStdio(t *testing.T, apiURL, token string) (*sdk.ClientSession, *bytes.Buffer, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	var stderr bytes.Buffer
	command := stdioCommand(ctx, apiURL, token)
	command.Stderr = &stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "protocol-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: command}, nil)
	if err != nil {
		return nil, &stderr, err
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, &stderr, nil
}

func stdioCommand(ctx context.Context, apiURL, token string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioProtocolHelper$")
	command.Env = append(os.Environ(),
		"GOTHAM_TEST_MCP_STDIO=1",
		"GOTHAM_MCP_URL="+apiURL,
		"GOTHAM_MCP_TOKEN="+token,
		"GOTHAM_MCP_TEAM_ID="+protocolTeam,
	)
	return command
}

// runStdioRaw writes frames one at a time and reads one response per request
// frame, returning the raw stdout transcript.
func runStdioRaw(t *testing.T, apiURL, token string, frames []string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := stdioCommand(ctx, apiURL, token)
	stdin, err := command.StdinPipe()
	if err != nil {
		return "", "", err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", "", err
	}
	scanner := bufio.NewScanner(stdout)
	var transcript strings.Builder
	for _, frame := range frames {
		if _, err := fmt.Fprintln(stdin, frame); err != nil {
			return "", "", err
		}
		if strings.Contains(frame, "notifications/initialized") {
			continue
		}
		if !scanner.Scan() {
			return transcript.String(), "", errors.New("missing protocol response")
		}
		transcript.WriteString(scanner.Text())
		transcript.WriteByte('\n')
	}
	_ = stdin.Close()
	for scanner.Scan() {
		transcript.WriteString(scanner.Text())
		transcript.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return transcript.String(), "", err
	}
	err = command.Wait()
	return transcript.String(), stderr.String(), err
}
