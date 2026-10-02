package builds

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewDockerTransport(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantBase string
		wantErr  bool
	}{
		{name: "default", host: "", wantBase: "http://docker"},
		{name: "unix scheme", host: "unix:///var/run/docker.sock", wantBase: "http://docker"},
		{name: "socket path", host: "/var/run/docker.sock", wantBase: "http://docker"},
		{name: "tcp", host: "tcp://127.0.0.1:2375", wantBase: "http://127.0.0.1:2375"},
		{name: "http", host: "http://127.0.0.1:2375", wantBase: "http://127.0.0.1:2375"},
		{name: "empty tcp", host: "tcp://", wantErr: true},
		{name: "unsupported", host: "ftp://daemon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, base, err := newDockerTransport(tt.host)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("newDockerTransport(%q) = (%v, %q); want error", tt.host, transport, base)
				}
				return
			}
			if err != nil {
				t.Fatalf("newDockerTransport(%q): %v", tt.host, err)
			}
			if base != tt.wantBase {
				t.Errorf("base = %q; want %q", base, tt.wantBase)
			}
		})
	}
}

func TestDecodeBuildStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"stream":"Step 1/2 : FROM scratch\n"}`,
		`{"stream":"Step 2/2 : COPY hello.txt /hello.txt\n"}`,
		`{"aux":{"ID":"sha256:abc"}}`,
	}, "\n")
	out := decodeBuildStream(strings.NewReader(stream))
	if out.err != nil {
		t.Fatalf("decodeBuildStream error = %v", out.err)
	}
	if out.digest != "sha256:abc" {
		t.Errorf("digest = %q; want sha256:abc", out.digest)
	}
	if !strings.Contains(out.logs, "Step 2/2") {
		t.Errorf("logs = %q; want the build steps", out.logs)
	}
}

func TestDecodeBuildStreamError(t *testing.T) {
	stream := `{"stream":"boom\n"}` + "\n" +
		`{"errorDetail":{"message":"failed to solve"},"error":"failed to solve"}`
	out := decodeBuildStream(strings.NewReader(stream))
	if out.err == nil || out.err.Error() != "failed to solve" {
		t.Fatalf("error = %v; want failed to solve", out.err)
	}
	if !strings.Contains(out.logs, "boom") {
		t.Errorf("logs = %q; want output captured before the error", out.logs)
	}
}

// requireDocker returns a builder for the local daemon, skipping when the
// daemon is unreachable so GOTHAM_E2E runs stay portable.
func requireDocker(t *testing.T) *LocalDockerBuilder {
	t.Helper()
	builder, err := NewLocalDockerBuilder()
	if err != nil {
		t.Fatalf("NewLocalDockerBuilder: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, builder.baseURL+"/_ping", nil)
	if err != nil {
		t.Fatalf("ping request: %v", err)
	}
	response, err := builder.http.Do(request)
	if err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Skipf("docker daemon ping status %d", response.StatusCode)
	}
	return builder
}

func e2eDockerfileDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nCOPY hello.txt /hello.txt\n")
	writeTestFile(t, filepath.Join(dir, "hello.txt"), "hello gotham\n")
	return dir
}

func TestLocalDockerBuilderE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live Docker builds")
	}
	builder := requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	contextTar, err := buildContextTar(e2eDockerfileDir(t), nil)
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	result, err := builder.Build(ctx, contextTar, ImageBuildOptions{
		Tag:        "gotham/e2e-local:" + uuid.NewString(),
		Dockerfile: "Dockerfile",
	})
	if err != nil {
		t.Fatalf("Build: %v (logs: %s)", err, result.Logs)
	}
	if result.Digest == "" {
		t.Error("Build returned an empty digest")
	}
}

func TestDockerfileEngineE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live Docker builds")
	}
	builder := requireDocker(t)
	registry := NewRegistry(builder)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ref, err := registry.Build(ctx, testOptions(e2eDockerfileDir(t)))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineDockerfile {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineDockerfile)
	}
	if ref.Tag != ImageTag(testAppID, testDeployID) {
		t.Errorf("Tag = %q; want %q", ref.Tag, ImageTag(testAppID, testDeployID))
	}
	if ref.Digest == "" {
		t.Error("Build returned an empty digest")
	}
}

func TestStaticEngineE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live Docker builds")
	}
	builder := requireDocker(t)
	registry := NewRegistry(builder)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "index.html"), "<h1>hello gotham</h1>\n")

	ref, err := registry.Build(ctx, testOptions(dir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineStatic {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineStatic)
	}
	if ref.Digest == "" {
		t.Error("Build returned an empty digest")
	}
}

// TestLocalDockerBuilderDockerHost pins the endpoint handed to toolchain CLIs:
// the toolchain runs with a stripped environment, so the builder must carry a
// usable DOCKER_HOST itself.
func TestLocalDockerBuilderDockerHost(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{host: "", want: ""},
		{host: "/var/run/docker.sock", want: "unix:///var/run/docker.sock"},
		{host: "unix:///run/docker.sock", want: "unix:///run/docker.sock"},
		{host: "tcp://127.0.0.1:2375", want: "tcp://127.0.0.1:2375"},
	} {
		builder, err := NewLocalDockerBuilderWithHost(tc.host)
		if err != nil {
			t.Fatalf("NewLocalDockerBuilderWithHost(%q): %v", tc.host, err)
		}
		if builder.dockerHost != tc.want {
			t.Errorf("dockerHost(%q) = %q; want %q", tc.host, builder.dockerHost, tc.want)
		}
	}
}
