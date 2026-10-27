package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/servers"
)

const (
	fileRootVolumes = "volumes"
	fileRootCompose = "compose"
)

func TestFilesEndToEnd(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "true")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	base := newFilesHarness(t, ctx)
	client := base.httpClient

	serverPath := "/v1/servers/" + base.serverID.String() + "/files"

	roots := base.getJSON(t, client, serverPath+"/roots?root=")
	rootList := roots["roots"].([]any)
	if len(rootList) != 2 {
		t.Fatalf("roots = %v, want the volumes and compose roots", rootList)
	}
	writable := map[string]bool{}
	for _, raw := range rootList {
		root := raw.(map[string]any)
		writable[root["name"].(string)] = root["writable"].(bool)
	}
	if !writable[fileRootVolumes] || writable[fileRootCompose] {
		t.Fatalf("root writability = %v, want volumes writable and compose read-only", writable)
	}

	if status, _ := base.do(t, client, http.MethodPut, serverPath+"/content?root="+fileRootVolumes+"&path=hello.txt", "hi\n", nil); status != http.StatusPreconditionRequired {
		t.Fatalf("PUT without If-Match = %d, want 428", status)
	}

	upload := base.upload(t, client, serverPath+"/upload?root="+fileRootVolumes+"&path=", "hello.txt", "hello\n")
	results := upload["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("upload results = %v, want one", results)
	}
	if result := results[0].(map[string]any); result["entry"] == nil {
		t.Fatalf("upload result has no entry: %v", result)
	}

	content := base.getJSON(t, client, serverPath+"/content?root="+fileRootVolumes+"&path=hello.txt")
	if got := content["content"].(string); got != "hello\n" {
		t.Fatalf("content = %q, want %q", got, "hello\n")
	}
	etag := content["etag"].(string)
	if etag == "" {
		t.Fatal("read returned an empty etag")
	}

	status, body := base.do(t, client, http.MethodPut, serverPath+"/content?root="+fileRootVolumes+"&path=hello.txt", "hello world\n",
		map[string]string{"If-Match": etag})
	if status != http.StatusOK {
		t.Fatalf("PUT with matching If-Match = %d (%s), want 200", status, body)
	}
	var writeEnvelope map[string]any
	if err := json.Unmarshal([]byte(body), &writeEnvelope); err != nil {
		t.Fatalf("decode write envelope: %v", err)
	}
	newEtag := writeEnvelope["entry"].(map[string]any)["etag"].(string)
	if newEtag == "" || newEtag == etag {
		t.Fatalf("new etag = %q, want a fresh etag over %q", newEtag, etag)
	}

	if status, _ := base.do(t, client, http.MethodPut, serverPath+"/content?root="+fileRootVolumes+"&path=hello.txt", "boom\n",
		map[string]string{"If-Match": etag}); status != http.StatusConflict {
		t.Fatalf("PUT with stale If-Match = %d, want 409", status)
	}
	if got := base.getJSON(t, client, serverPath+"/content?root="+fileRootVolumes+"&path=hello.txt")["content"].(string); got != "hello world\n" {
		t.Fatalf("content after the rejected save = %q, want %q", got, "hello world\n")
	}

	if status, _ := base.do(t, client, http.MethodPut, serverPath+"/content?root="+fileRootCompose+"&path=nope.txt&overwrite=true", "x", nil); status != http.StatusForbidden {
		t.Fatalf("PUT into the read-only root = %d, want 403", status)
	}

	status, ticketBody := base.do(t, client, http.MethodPost, serverPath+"/download-ticket?root="+fileRootVolumes+"&path=hello.txt", "", nil)
	if status != http.StatusCreated {
		t.Fatalf("download-ticket = %d (%s), want 201", status, ticketBody)
	}
	var ticketEnvelope map[string]string
	if err := json.Unmarshal([]byte(ticketBody), &ticketEnvelope); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	downloadURL := serverPath + "/download?ticket=" + ticketEnvelope["ticket"]
	status, downloaded := base.do(t, client, http.MethodGet, downloadURL, "", nil)
	if status != http.StatusOK {
		t.Fatalf("download = %d (%s), want 200", status, downloaded)
	}
	if downloaded != "hello world\n" {
		t.Fatalf("downloaded body = %q, want %q", downloaded, "hello world\n")
	}
	if status, _ := base.do(t, client, http.MethodGet, downloadURL, "", nil); status != http.StatusNotFound {
		t.Fatalf("second download with the same ticket = %d, want 404", status)
	}

	if status, body := base.do(t, client, http.MethodDelete, serverPath+"/entry?root="+fileRootVolumes+"&path=hello.txt", "", nil); status != http.StatusNoContent {
		t.Fatalf("DELETE = %d (%s), want 204", status, body)
	}
	if status, _ := base.do(t, client, http.MethodGet, serverPath+"/stat?root="+fileRootVolumes+"&path=hello.txt", "", nil); status != http.StatusNotFound {
		t.Fatalf("stat after delete = %d, want 404", status)
	}
	if _, err := os.Stat(filepath.Join(base.writableRoot, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("file still on disk after delete: err=%v", err)
	}

	if status, body := base.do(t, client, http.MethodPost, serverPath+"/mkdir",
		`{"root":"`+fileRootVolumes+`","path":"data"}`, nil); status != http.StatusCreated {
		t.Fatalf("mkdir data = %d (%s), want 201", status, body)
	}
	for name, content := range map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n"} {
		base.upload(t, client, serverPath+"/upload?root="+fileRootVolumes+"&path=data/", name, content)
	}
	status, body = base.do(t, client, http.MethodPost, serverPath+"/compress",
		`{"root":"`+fileRootVolumes+`","sources":["data"],"destination":"data.zip","format":"zip"}`, nil)
	if status != http.StatusOK {
		t.Fatalf("compress = %d (%s), want 200", status, body)
	}
	var compressEnvelope map[string]any
	if err := json.Unmarshal([]byte(body), &compressEnvelope); err != nil {
		t.Fatalf("decode compress: %v (%s)", err, body)
	}
	if name := compressEnvelope["entry"].(map[string]any)["name"].(string); name != "data.zip" {
		t.Fatalf("compress entry name = %q, want data.zip", name)
	}
	if status, body := base.do(t, client, http.MethodDelete,
		serverPath+"/entry?root="+fileRootVolumes+"&path=data&recursive=true", "", nil); status != http.StatusNoContent {
		t.Fatalf("delete data = %d (%s), want 204", status, body)
	}
	if status, body := base.do(t, client, http.MethodPost, serverPath+"/extract",
		`{"root":"`+fileRootVolumes+`","source":"data.zip","destination":""}`, nil); status != http.StatusNoContent {
		t.Fatalf("extract = %d (%s), want 204", status, body)
	}
	if got := base.getJSON(t, client, serverPath+"/content?root="+fileRootVolumes+"&path=data/a.txt")["content"].(string); got != "alpha\n" {
		t.Fatalf("restored content = %q, want %q", got, "alpha\n")
	}
	if _, err := os.Stat(filepath.Join(base.writableRoot, "data", "b.txt")); err != nil {
		t.Fatalf("restored file missing on disk: %v", err)
	}

	t.Logf("files e2e ok: node=%s root=%s", base.nodeID, base.writableRoot)
}

func TestFilesEndToEndDisabled(t *testing.T) {
	requireE2E(t)
	t.Setenv(files.FeatureEnv, "false")

	registry := &staticRegistry{server: &servers.Server{ID: uuid.New(), Name: "disabled-node"}}
	svc := files.NewService(files.Config{Registry: registry, Logger: testLogger(t)})
	api := chi.NewRouter()
	files.Mount(api, passthroughAuth, nil, svc, testLogger(t))
	httpServer := httptest.NewServer(api)
	t.Cleanup(httpServer.Close)

	url := httpServer.URL + "/v1/servers/" + registry.server.ID.String() + "/files/roots"
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET roots: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("disabled roots = %d, want 503", response.StatusCode)
	}
}

type filesHarness struct {
	nodeID       string
	serverID     uuid.UUID
	writableRoot string
	composeRoot  string
	httpClient   *http.Client
	baseURL      string
}

func newFilesHarness(t *testing.T, ctx context.Context) *filesHarness {
	t.Helper()
	logger := testLogger(t)

	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client for %s: %v", e2eDockerSock(), err)
	}
	writableRoot := canonicalTempDir(t)
	composeRoot := canonicalTempDir(t)
	certDir := canonicalTempDir(t)
	fileServer, err := agent.NewFileServer(agent.Config{
		ManagedVolumeRoot: writableRoot,
		ComposeRoot:       composeRoot,
		CertDir:           certDir,
		Files:             true,
	}, logger)
	if err != nil {
		t.Fatalf("agent file server: %v", err)
	}
	t.Cleanup(func() { _ = fileServer.Close() })

	nodeID := "files-e2e-" + uuid.New().String()[:8]
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID, agent.WithFileService(fileServer))

	serverID := uuid.New()
	registry := &staticRegistry{server: &servers.Server{ID: serverID, Name: "files-e2e", IP: agentAddr, NodeID: &nodeID}}
	svc := files.NewService(files.Config{
		Registry: registry,
		Logger:   logger,
		Dial: func(dialCtx context.Context, _ *servers.Server) (files.FileClient, error) {
			return servers.DialFileClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		},
	})

	api := chi.NewRouter()
	files.Mount(api, passthroughAuth, nil, svc, logger)
	httpServer := httptest.NewServer(api)
	t.Cleanup(httpServer.Close)

	return &filesHarness{
		nodeID:       nodeID,
		serverID:     serverID,
		writableRoot: writableRoot,
		composeRoot:  composeRoot,
		httpClient:   httpServer.Client(),
		baseURL:      httpServer.URL,
	}
}

func passthroughAuth(next http.Handler) http.Handler { return next }

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	return dir
}

func (h *filesHarness) do(t *testing.T, client *http.Client, method, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.baseURL+path, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return response.StatusCode, string(payload)
}

func (h *filesHarness) getJSON(t *testing.T, client *http.Client, path string) map[string]any {
	t.Helper()
	status, body := h.do(t, client, http.MethodGet, path, "", nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d (%s), want 200", path, status, body)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode %s: %v (%s)", path, err, body)
	}
	return decoded
}

func (h *filesHarness) upload(t *testing.T, client *http.Client, path, name, content string) map[string]any {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.baseURL+path, &buffer)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("upload read: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d (%s), want 200", response.StatusCode, payload)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode upload: %v (%s)", err, payload)
	}
	return decoded
}
