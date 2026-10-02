package builds

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

	contextTar, err := buildContextTar(contextSpec{root: e2eDockerfileDir(t)})
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

// TestLocalDockerBuilderToolchainPassesDockerHost runs a fake railpack through
// buildToolchain and checks the builder hands the CLI its DOCKER_HOST, since
// the toolchain environment is stripped of the parent's variables.
func TestLocalDockerBuilderToolchainPassesDockerHost(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "docker-host")
	fakeCLI(t, railpackCLI, fmt.Sprintf(`printf '%%s' "$DOCKER_HOST" > '%s'`, envFile))
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	var digestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		digestPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":          "sha256:config",
			"RepoDigests": []string{"gotham/app@sha256:feedface"},
		})
	}))
	t.Cleanup(server.Close)

	builder, err := NewLocalDockerBuilderWithHost(server.URL)
	if err != nil {
		t.Fatalf("new builder: %v", err)
	}

	srcDir := t.TempDir()
	writeTestFile(t, filepath.Join(srcDir, "package.json"), "{}\n")
	contextTar, err := buildContextTar(contextSpec{root: srcDir})
	if err != nil {
		t.Fatalf("build context: %v", err)
	}

	result, err := builder.Build(context.Background(), contextTar, ImageBuildOptions{
		Tag:    "gotham/app:dep",
		Engine: EngineRailpack,
	})
	if err != nil {
		t.Fatalf("toolchain build: %v", err)
	}
	if result.Digest != "gotham/app@sha256:feedface" {
		t.Errorf("digest = %q; want gotham/app@sha256:feedface", result.Digest)
	}
	if digestPath != "/images/gotham/app:dep/json" {
		t.Errorf("digest path = %q", digestPath)
	}
	if got := readFakeCLI(t, envFile); got != server.URL {
		t.Errorf("DOCKER_HOST = %q; want %q", got, server.URL)
	}
}

// TestDockerfileBuildObeysDockerignoreE2E is the live C1-1 regression: a
// credential excluded by the repository's .dockerignore must not appear in the
// built image, while an ordinary file must. It exports the image rootfs and
// inspects the archive, so it needs no shell in the built image.
func TestDockerfileBuildObeysDockerignoreE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live Docker builds")
	}
	builder := requireDocker(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nCOPY . /app\n")
	writeTestFile(t, filepath.Join(dir, ".dockerignore"), ".env\n")
	writeTestFile(t, filepath.Join(dir, ".env"), "TOKEN=leaked\n")
	writeTestFile(t, filepath.Join(dir, "app.txt"), "ok\n")

	contextTar, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	ref := "gotham/e2e-ignore:" + uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := builder.Build(ctx, contextTar, ImageBuildOptions{Tag: ref, Dockerfile: "Dockerfile"}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "-f", ref).Run() })

	files := exportedRootfsFiles(t, ref, "app")
	if _, leaked := files["app/.env"]; leaked {
		t.Error(".dockerignore-ignored .env leaked into the image")
	}
	if _, ok := files["app/app.txt"]; !ok {
		t.Error("non-ignored app.txt is missing from the image")
	}
}

// TestDockerfileBuildFailedStepLeavesNoContainerE2E is the live C1-5
// regression: a failed RUN step must not leave an intermediate container.
func TestDockerfileBuildFailedStepLeavesNoContainerE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live Docker builds")
	}
	builder := requireDocker(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nRUN [\"/bin/false\"]\n")

	contextTar, err := buildContextTar(contextSpec{root: dir})
	if err != nil {
		t.Fatalf("buildContextTar: %v", err)
	}
	before := dockerContainerIDs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err = builder.Build(ctx, contextTar, ImageBuildOptions{Tag: "gotham/e2e-fail:" + uuid.NewString(), Dockerfile: "Dockerfile"})
	if err == nil {
		t.Fatal("failing Dockerfile built successfully")
	}
	for id := range dockerContainerIDs(t) {
		if _, existed := before[id]; !existed {
			t.Errorf("failed build left intermediate container %s", id)
		}
	}
}

// dockerCLI runs a docker subcommand and returns its trimmed stdout.
func dockerCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		t.Skipf("docker %s unavailable: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// dockerContainerIDs lists every container id on the daemon.
func dockerContainerIDs(t *testing.T) map[string]struct{} {
	t.Helper()
	ids := make(map[string]struct{})
	for _, line := range strings.Split(dockerCLI(t, "ps", "-aq"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids[line] = struct{}{}
		}
	}
	return ids
}

// exportedRootfsFiles exports an image's flattened rootfs and returns the paths
// under prefix. The temporary container is removed on cleanup.
func exportedRootfsFiles(t *testing.T, image, prefix string) map[string]struct{} {
	t.Helper()
	id := dockerCLI(t, "create", image, "/true")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	cmd := exec.Command("docker", "export", id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("export pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("export start: %v", err)
	}
	files := make(map[string]struct{})
	reader := tar.NewReader(stdout)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read export archive: %v", err)
		}
		name := strings.TrimPrefix(header.Name, "./")
		if strings.HasPrefix(name, prefix+"/") {
			files[name] = struct{}{}
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("docker export: %v", err)
	}
	return files
}
