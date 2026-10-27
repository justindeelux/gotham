package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	agentfiles "github.com/justindeelux/gotham/agent/files"
	"github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/servers"
)

type transferRegistry struct {
	items map[uuid.UUID]*servers.Server
}

func (r *transferRegistry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	server, ok := r.items[id]
	if !ok {
		return nil, servers.ErrNotFound
	}
	return server, nil
}

type transferAgent struct {
	address   string
	authority *servers.Authority
	nodeID    string
}

type transferHarness struct {
	sourceID      uuid.UUID
	destinationID uuid.UUID
	sourceRoot    string
	destRoot      string
	baseURL       string
	client        *http.Client
}

func newTransferHarness(t *testing.T, ctx context.Context) *transferHarness {
	t.Helper()
	logger := testLogger(t)
	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client for %s: %v", e2eDockerSock(), err)
	}

	agents := make(map[uuid.UUID]transferAgent)
	registry := &transferRegistry{items: make(map[uuid.UUID]*servers.Server)}
	roots := make(map[uuid.UUID]string)
	for _, node := range []string{"source", "destination"} {
		root := canonicalTempDir(t)
		fileServer, err := agentfiles.NewFileServer(agentfiles.Config{
			ManagedVolumeRoot: root,
			ComposeRoot:       canonicalTempDir(t),
			CertDir:           canonicalTempDir(t),
			AgentStateRoot:    t.TempDir(),
		}, logger)
		if err != nil {
			t.Fatalf("%s agent file server: %v", node, err)
		}
		t.Cleanup(func() { _ = fileServer.Close() })
		nodeID := "files-transfer-" + node + "-" + uuid.New().String()[:8]
		address, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID, agent.WithFileService(fileServer))
		id := uuid.New()
		registry.items[id] = &servers.Server{ID: id, Name: node, IP: address, NodeID: &nodeID}
		agents[id] = transferAgent{address: address, authority: authority, nodeID: nodeID}
		roots[id] = root
	}

	svc := files.NewService(files.Config{
		Registry: registry,
		Logger:   logger,
		Dial: func(dialCtx context.Context, server *servers.Server) (files.FileClient, error) {
			target := agents[server.ID]
			return servers.DialFileClient(dialCtx, target.address, target.authority, servers.WithDockerServerName(target.nodeID))
		},
	})

	var sourceID, destinationID uuid.UUID
	for id, server := range registry.items {
		if server.Name == "source" {
			sourceID = id
		} else {
			destinationID = id
		}
	}
	api := chi.NewRouter()
	authorize := files.TransferAuthorizerFunc(func(context.Context, uuid.UUID, uuid.UUID) error { return nil })
	files.Mount(api, passthroughAuth, authorize, svc, logger)
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	return &transferHarness{
		sourceID:      sourceID,
		destinationID: destinationID,
		sourceRoot:    roots[sourceID],
		destRoot:      roots[destinationID],
		baseURL:       server.URL,
		client:        server.Client(),
	}
}

func (h *transferHarness) request(t *testing.T, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return response.StatusCode, string(payload)
}

func (h *transferHarness) streamTransfer(t *testing.T, body string) []files.TransferEvent {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.baseURL+"/v1/servers/"+h.sourceID.String()+"/files/transfers", strings.NewReader(body))
	if err != nil {
		t.Fatalf("transfer request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("transfer = %d (%s), want 200", response.StatusCode, payload)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/x-ndjson" {
		t.Fatalf("content-type = %q", contentType)
	}
	var events []files.TransferEvent
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var event files.TransferEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	return events
}

func transferSummary(t *testing.T, events []files.TransferEvent) files.TransferSummary {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no transfer events")
	}
	last := events[len(events)-1]
	if last.Type != files.TransferEventSummary || last.Summary == nil {
		t.Fatalf("last event = %+v, want the terminal summary", last)
	}
	return *last.Summary
}

// transferResultByName returns the streamed result for a top-level entry and
// fails when the entry was streamed more than once.
func transferResultByName(t *testing.T, events []files.TransferEvent, name string) files.TransferEvent {
	t.Helper()
	var found []files.TransferEvent
	for index, event := range events {
		if event.Type != files.TransferEventResult || event.Name != name {
			continue
		}
		if index == len(events)-1 {
			t.Fatalf("result for %q arrived after the summary", name)
		}
		found = append(found, event)
	}
	if len(found) != 1 {
		t.Fatalf("result events for %q = %+v, want exactly one before the summary", name, found)
	}
	return found[0]
}

func TestFilesCrossNodeTransferEndToEnd(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newTransferHarness(t, ctx)
	transferPath := "/v1/servers/" + h.sourceID.String() + "/files/transfers"
	planPath := transferPath + "/plan"

	writeSource := func(t *testing.T, rel, content string) {
		t.Helper()
		full := filepath.Join(h.sourceRoot, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	body := func(sources ...string) string {
		quoted := make([]string, 0, len(sources))
		for _, source := range sources {
			quoted = append(quoted, `"`+source+`"`)
		}
		return `{"source_root":"volumes","source_directory":"","sources":[` + strings.Join(quoted, ",") +
			`],"destination":{"server_id":"` + h.destinationID.String() + `","root":"volumes","path":""}}`
	}

	writeSource(t, "hello.txt", "hello\n")
	status, planBody := h.request(t, planPath, body("hello.txt"))
	if status != http.StatusOK {
		t.Fatalf("plan = %d (%s), want 200", status, planBody)
	}
	var plan files.TransferPlan
	if err := json.Unmarshal([]byte(planBody), &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.Mode != files.TransferModeDirect || len(plan.Sources) != 1 || plan.Sources[0].Conflict {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(h.destRoot, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("plan created the destination file: %v", err)
	}

	directEvents := h.streamTransfer(t, body("hello.txt"))
	summary := transferSummary(t, directEvents)
	if summary.Status != files.TransferStatusCopied || summary.Copied != 1 {
		t.Fatalf("direct summary = %+v", summary)
	}
	if result := transferResultByName(t, directEvents, "hello.txt"); result.Status != files.TransferStatusCopied {
		t.Fatalf("direct result = %+v", result)
	}
	copied, err := os.ReadFile(filepath.Join(h.destRoot, "hello.txt"))
	if err != nil || string(copied) != "hello\n" {
		t.Fatalf("destination content = %q, err %v", copied, err)
	}
	source, err := os.ReadFile(filepath.Join(h.sourceRoot, "hello.txt"))
	if err != nil || string(source) != "hello\n" {
		t.Fatalf("source content changed: %q, err %v", source, err)
	}

	conflictEvents := h.streamTransfer(t, body("hello.txt"))
	summary = transferSummary(t, conflictEvents)
	if summary.Conflicts != 1 || summary.Copied != 0 {
		t.Fatalf("conflict summary = %+v", summary)
	}
	if result := transferResultByName(t, conflictEvents, "hello.txt"); result.Status != files.TransferStatusConflict {
		t.Fatalf("conflict result = %+v", result)
	}
	if copied, _ := os.ReadFile(filepath.Join(h.destRoot, "hello.txt")); string(copied) != "hello\n" {
		t.Fatalf("a conflicting destination was overwritten: %q", copied)
	}

	writeSource(t, "data/a.txt", "alpha\n")
	writeSource(t, "data/b.txt", "beta\n")
	if err := os.Symlink("../hello.txt", filepath.Join(h.sourceRoot, "data", "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	bundleBody := body("data")
	status, planBody = h.request(t, planPath, bundleBody)
	if status != http.StatusOK {
		t.Fatalf("bundle plan = %d (%s), want 200", status, planBody)
	}
	if err := json.Unmarshal([]byte(planBody), &plan); err != nil {
		t.Fatalf("decode bundle plan: %v", err)
	}
	if plan.Mode != files.TransferModeBundle {
		t.Fatalf("bundle plan mode = %q", plan.Mode)
	}
	events := h.streamTransfer(t, bundleBody)
	summary = transferSummary(t, events)
	if summary.Status != files.TransferStatusCopied || summary.Copied != 1 || summary.SkippedCount != 1 {
		t.Fatalf("bundle summary = %+v", summary)
	}
	if result := transferResultByName(t, events, "data"); result.Status != files.TransferStatusCopied {
		t.Fatalf("bundle result = %+v", result)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if got, err := os.ReadFile(filepath.Join(h.destRoot, "data", name)); err != nil || string(got) != map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n"}[name] {
			t.Fatalf("destination %s = %q, err %v", name, got, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(h.destRoot, ".gotham-transfer")); err == nil && len(entries) != 0 {
		t.Fatalf("destination staging was not cleaned: %v", entries)
	}

	for _, path := range []string{planPath, transferPath} {
		sameNode := `{"source_root":"volumes","source_directory":"","sources":["hello.txt"],"destination":{"server_id":"` + h.sourceID.String() + `","root":"volumes","path":""}}`
		if status, payload := h.request(t, path, sameNode); status != http.StatusBadRequest {
			t.Fatalf("same-node %s = %d (%s), want 400", path, status, payload)
		}
	}
	forged := `{"source_root":"volumes","source_directory":"","sources":["hello.txt"],"destination":{"server_id":"` + uuid.NewString() + `","root":"volumes","path":""}}`
	if status, payload := h.request(t, planPath, forged); status != http.StatusNotFound {
		t.Fatalf("forged destination = %d (%s), want 404", status, payload)
	}
}
