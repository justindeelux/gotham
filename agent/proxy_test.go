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

// newTestProxyServer wires a ProxyServer rooted in a temp directory and
// pinging the given URL.
func newTestProxyServer(t *testing.T, pingURL string) (*ProxyServer, string) {
	t.Helper()
	root := t.TempDir()
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
