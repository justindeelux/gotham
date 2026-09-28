package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// canonicalTempDir returns a temp directory with OS-level symlinks resolved
// (macOS /var -> /private/var), so the agent's no-follow traversal from "/"
// sees real components only. Production roots are canonical by construction.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return dir
}

// newTestProxyServer wires a ProxyServer rooted in a temp directory and
// pinging the given URL.
func newTestProxyServer(t *testing.T, pingURL string) (*ProxyServer, string) {
	t.Helper()
	root := canonicalTempDir(t)
	server := NewProxyServer(ProxyServerConfig{
		Root:        root,
		PingURL:     pingURL,
		PingTimeout: time.Second,
		Logger:      discardLogger(),
	})
	return server, root
}

func request(files map[string]string, verify bool) *agentv1.WriteProxyConfigRequest {
	request := &agentv1.WriteProxyConfigRequest{Verify: verify}
	for path, content := range files {
		request.Files = append(request.Files, &agentv1.ProxyConfigFile{Path: path, Content: []byte(content)})
	}
	return request
}

func TestWriteProxyConfigWritesFilesAtomically(t *testing.T) {
	server, root := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	response, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml":        "entryPoints: {}\n",
		"dynamic/gotham.yml": "http: {}\n",
	}, false))
	if err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}
	if response.GetReloaded() || response.GetPingError() != "" {
		t.Errorf("unverified response = %#v", response)
	}
	if len(response.GetWritten()) != 2 {
		t.Fatalf("written = %v, want two paths", response.GetWritten())
	}

	static, err := os.ReadFile(filepath.Join(root, "traefik.yml"))
	if err != nil {
		t.Fatalf("read static config: %v", err)
	}
	if string(static) != "entryPoints: {}\n" {
		t.Errorf("static content = %q", static)
	}
	dynamic, err := os.ReadFile(filepath.Join(root, "dynamic", "gotham.yml"))
	if err != nil {
		t.Fatalf("read dynamic config: %v", err)
	}
	if string(dynamic) != "http: {}\n" {
		t.Errorf("dynamic content = %q", dynamic)
	}
	info, err := os.Stat(filepath.Join(root, "traefik.yml"))
	if err != nil {
		t.Fatalf("stat static config: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestWriteProxyConfigOverwritesExistingDocument(t *testing.T) {
	server, root := newTestProxyServer(t, "")
	path := filepath.Join(root, "dynamic", "gotham.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old content that is longer"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"dynamic/gotham.yml": "new",
	}, false)); err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("content = %q, want new", data)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	} else if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestWriteProxyConfigVerifiesPing(t *testing.T) {
	ping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ping.Close()

	server, _ := newTestProxyServer(t, ping.URL)
	response, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml": "static",
	}, true))
	if err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}
	if !response.GetReloaded() {
		t.Fatalf("reloaded = false, ping_error = %q", response.GetPingError())
	}
	if response.GetPingError() != "" {
		t.Errorf("ping_error = %q, want empty", response.GetPingError())
	}
}

func TestWriteProxyConfigReportsPingFailure(t *testing.T) {
	ping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ping.Close()

	server, _ := newTestProxyServer(t, ping.URL)
	response, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml": "static",
	}, true))
	if err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}
	if response.GetReloaded() {
		t.Fatal("reloaded = true despite a 503 ping")
	}
	if !strings.Contains(response.GetPingError(), "status 503") {
		t.Errorf("ping_error = %q, want the HTTP status", response.GetPingError())
	}
}

func TestWriteProxyConfigReportsUnreachablePing(t *testing.T) {
	server, _ := newTestProxyServer(t, "http://127.0.0.1:1/ping")
	response, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml": "static",
	}, true))
	if err != nil {
		t.Fatalf("WriteProxyConfig: %v", err)
	}
	if response.GetReloaded() || response.GetPingError() == "" {
		t.Fatalf("response = %#v, want an unreachable ping", response)
	}
}

func TestWriteProxyConfigRejectsUnsafePaths(t *testing.T) {
	server, _ := newTestProxyServer(t, "")
	paths := []string{
		"",
		" ",
		"/etc/passwd",
		"../escape.yml",
		"dynamic/../../escape.yml",
		"..",
		".",
		`windows\path.yml`,
	}
	for _, path := range paths {
		_, err := server.WriteProxyConfig(context.Background(), request(map[string]string{path: "x"}, false))
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("path %q: code = %v, want InvalidArgument (err %v)", path, status.Code(err), err)
		}
	}
}

func TestWriteProxyConfigBounds(t *testing.T) {
	server, _ := newTestProxyServer(t, "")

	if _, err := server.WriteProxyConfig(context.Background(), &agentv1.WriteProxyConfigRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("empty request: code = %v, want InvalidArgument", status.Code(err))
	}

	tooMany := &agentv1.WriteProxyConfigRequest{}
	for i := 0; i <= maxProxyFiles; i++ {
		tooMany.Files = append(tooMany.Files, &agentv1.ProxyConfigFile{Path: "file.yml", Content: []byte("x")})
	}
	if _, err := server.WriteProxyConfig(context.Background(), tooMany); status.Code(err) != codes.InvalidArgument {
		t.Errorf("too many files: code = %v, want InvalidArgument", status.Code(err))
	}

	oversized := &agentv1.WriteProxyConfigRequest{
		Files: []*agentv1.ProxyConfigFile{{Path: "big.yml", Content: make([]byte, maxProxyFileSize+1)}},
	}
	if _, err := server.WriteProxyConfig(context.Background(), oversized); status.Code(err) != codes.InvalidArgument {
		t.Errorf("oversized file: code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestSanitizeProxyPath(t *testing.T) {
	valid := map[string]string{
		"traefik.yml":        "traefik.yml",
		"dynamic/gotham.yml": "dynamic/gotham.yml",
		"./traefik.yml":      "traefik.yml",
	}
	for in, want := range valid {
		got, err := sanitizeProxyPath(in)
		if err != nil || got != want {
			t.Errorf("sanitizeProxyPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "/abs.yml", "../x", "a/../..", `a\b.yml`} {
		if _, err := sanitizeProxyPath(in); err == nil {
			t.Errorf("sanitizeProxyPath(%q) = nil error, want rejection", in)
		}
	}
}

// TestWriteProxyConfigRejectsSymlinkedParents proves the confinement promise:
// a symlinked parent (or root) is refused and nothing is written outside the
// proxy directory (BE-6.1 F7).
func TestWriteProxyConfigRejectsSymlinkedParents(t *testing.T) {
	root := canonicalTempDir(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "dynamic")); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}
	server := NewProxyServer(ProxyServerConfig{
		Root:        root,
		PingURL:     "http://127.0.0.1:1/ping",
		PingTimeout: time.Second,
		Logger:      discardLogger(),
	})
	_, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"dynamic/gotham.yml": "http: {}\n",
	}, false))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("symlinked parent: code = %v, want InvalidArgument (err %v)", status.Code(err), err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "gotham.yml")); !os.IsNotExist(statErr) {
		t.Errorf("write escaped the proxy directory: stat outside = %v", statErr)
	}

	rootLink := filepath.Join(canonicalTempDir(t), "root-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatalf("symlink root: %v", err)
	}
	linked := NewProxyServer(ProxyServerConfig{
		Root:        rootLink,
		PingURL:     "http://127.0.0.1:1/ping",
		PingTimeout: time.Second,
		Logger:      discardLogger(),
	})
	if _, err := linked.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml": "static",
	}, false)); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("symlinked root: code = %v, want InvalidArgument (err %v)", status.Code(err), err)
	}
}

// TestWriteProxyConfigValidatesWholeBatchBeforeWriting proves a rejected later
// entry cannot leave an earlier document installed (BE-6.1 F7).
func TestWriteProxyConfigValidatesWholeBatchBeforeWriting(t *testing.T) {
	server, root := newTestProxyServer(t, "")
	_, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml":   "static",
		"../escape.yml": "escape",
	}, false))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err %v)", status.Code(err), err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "traefik.yml")); !os.IsNotExist(statErr) {
		t.Errorf("first file was installed despite a rejected batch: stat err = %v", statErr)
	}
}

// TestWriteProxyConfigRejectsSymlinkedIntermediates proves no component under
// the root is followed: an intermediate symlink is rejected before anything is
// created on the other side (R3 trust anchor "/").
func TestWriteProxyConfigRejectsSymlinkedIntermediates(t *testing.T) {
	root := canonicalTempDir(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "dynamic")); err != nil {
		t.Fatalf("symlink intermediate: %v", err)
	}
	server := NewProxyServer(ProxyServerConfig{
		Root:        root,
		PingURL:     "http://127.0.0.1:1/ping",
		PingTimeout: time.Second,
		Logger:      discardLogger(),
	})

	// A missing child directory under the symlinked parent must not be created
	// outside the root.
	_, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"dynamic/sub/gotham.yml": "http: {}\n",
	}, false))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err %v)", status.Code(err), err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "sub")); !os.IsNotExist(statErr) {
		t.Errorf("directory escaped through the symlink: stat = %v", statErr)
	}
}

// TestWriteProxyConfigBatchRejectsSymlinkedLaterTarget proves the whole batch
// is validated before the first replacement: a valid first document must not
// replace existing content when a later document's parent is symlinked (R3).
func TestWriteProxyConfigBatchRejectsSymlinkedLaterTarget(t *testing.T) {
	root := canonicalTempDir(t)
	outside := t.TempDir()
	staticPath := filepath.Join(root, "traefik.yml")
	if err := os.WriteFile(staticPath, []byte("old static"), 0o644); err != nil {
		t.Fatalf("seed static: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dynamic")); err != nil {
		t.Fatalf("symlink dynamic: %v", err)
	}
	server := NewProxyServer(ProxyServerConfig{
		Root:        root,
		PingURL:     "http://127.0.0.1:1/ping",
		PingTimeout: time.Second,
		Logger:      discardLogger(),
	})

	_, err := server.WriteProxyConfig(context.Background(), request(map[string]string{
		"traefik.yml":        "new static",
		"dynamic/gotham.yml": "http: {}\n",
	}, false))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (err %v)", status.Code(err), err)
	}
	content, readErr := os.ReadFile(staticPath)
	if readErr != nil {
		t.Fatalf("read static: %v", readErr)
	}
	if string(content) != "old static" {
		t.Fatalf("first document was replaced despite a filesystem-invalid later entry: %q", content)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "gotham.yml")); !os.IsNotExist(statErr) {
		t.Errorf("write escaped through the symlinked parent: stat = %v", statErr)
	}
}
