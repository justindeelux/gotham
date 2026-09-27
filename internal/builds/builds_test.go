package builds

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Test identifiers shared across the package tests.
var (
	testAppID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testDeployID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// mockBuilder is a scriptable ImageBuilder that records its last call.
type mockBuilder struct {
	calls    int
	lastOpts ImageBuildOptions
	lastTar  []byte
	result   ImageBuildResult
	err      error
}

func (m *mockBuilder) Build(_ context.Context, contextTar []byte, opts ImageBuildOptions) (ImageBuildResult, error) {
	m.calls++
	m.lastOpts = opts
	m.lastTar = contextTar
	return m.result, m.err
}

func testOptions(repoDir string) BuildOptions {
	return BuildOptions{RepoDir: repoDir, AppID: testAppID, DeployID: testDeployID}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readContextTar decodes a build context tar into path -> contents, skipping
// directory entries.
func readContextTar(t *testing.T, data []byte) map[string]string {
	t.Helper()
	files := make(map[string]string)
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read context tar: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		files[header.Name] = string(content)
	}
	return files
}

func TestImageTag(t *testing.T) {
	got := ImageTag(testAppID, testDeployID)
	want := "gotham/11111111-1111-1111-1111-111111111111:22222222-2222-2222-2222-222222222222"
	if got != want {
		t.Fatalf("ImageTag = %q; want %q", got, want)
	}
}

func TestParseEngineKind(t *testing.T) {
	tests := []struct {
		in      string
		want    EngineKind
		wantErr bool
	}{
		{in: "", want: EngineAuto},
		{in: "auto", want: EngineAuto},
		{in: "DETECT", want: EngineAuto},
		{in: "dockerfile", want: EngineDockerfile},
		{in: " Dockerfile ", want: EngineDockerfile},
		{in: "railpack", want: EngineRailpack},
		{in: "nixpacks", want: EngineRailpack},
		{in: "buildpacks", want: EngineBuildpacks},
		{in: "herokuish", want: EngineBuildpacks},
		{in: "static", want: EngineStatic},
		{in: "wasm", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseEngineKind(tt.in)
		if tt.wantErr {
			if !errors.Is(err, ErrValidation) {
				t.Errorf("ParseEngineKind(%q) error = %v; want ErrValidation", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseEngineKind(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseEngineKind(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestRegistryBuildDockerfile(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "Dockerfile"), "FROM scratch\nCOPY app /app\n")
	writeTestFile(t, filepath.Join(repoDir, "app", "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(repoDir, ".git", "config"), "[core]\n")
	writeTestFile(t, filepath.Join(repoDir, "node_modules", "dep", "index.js"), "module.exports = {};\n")

	builder := &mockBuilder{result: ImageBuildResult{Digest: "sha256:deadbeef", Logs: "step 1\nstep 2\n"}}
	registry := NewRegistry(builder)

	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.LogWriter = &logs
	opts.BuildArgs = map[string]string{"GO_VERSION": "1.22"}
	opts.Labels = map[string]string{"org.gotham.app": "demo"}
	opts.Target = "runtime"

	ref, err := registry.Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineDockerfile {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineDockerfile)
	}
	if want := ImageTag(testAppID, testDeployID); ref.Tag != want {
		t.Errorf("Tag = %q; want %q", ref.Tag, want)
	}
	if ref.Digest != "sha256:deadbeef" {
		t.Errorf("Digest = %q; want sha256:deadbeef", ref.Digest)
	}
	if builder.lastOpts.Dockerfile != "Dockerfile" {
		t.Errorf("Dockerfile = %q; want Dockerfile", builder.lastOpts.Dockerfile)
	}
	if builder.lastOpts.Target != "runtime" {
		t.Errorf("Target = %q; want runtime", builder.lastOpts.Target)
	}
	if got := builder.lastOpts.BuildArgs["GO_VERSION"]; got != "1.22" {
		t.Errorf("BuildArgs[GO_VERSION] = %q; want 1.22", got)
	}
	if got := builder.lastOpts.Labels["org.gotham.app"]; got != "demo" {
		t.Errorf("Labels[org.gotham.app] = %q; want demo", got)
	}
	if logs.String() != "step 1\nstep 2\n" {
		t.Errorf("LogWriter = %q; want builder logs", logs.String())
	}

	files := readContextTar(t, builder.lastTar)
	if _, ok := files["Dockerfile"]; !ok {
		t.Error("context is missing Dockerfile")
	}
	if _, ok := files["app/main.go"]; !ok {
		t.Error("context is missing app/main.go")
	}
	if _, ok := files[".git/config"]; ok {
		t.Error("context must not contain .git")
	}
	if _, ok := files["node_modules/dep/index.js"]; !ok {
		t.Error("context should keep node_modules (only VCS/OS metadata is excluded)")
	}
}

func TestRegistryBuildStatic(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "index.html"), "<h1>hi</h1>\n")
	writeTestFile(t, filepath.Join(repoDir, "assets", "app.css"), "body{}\n")

	builder := &mockBuilder{result: ImageBuildResult{Digest: "sha256:cafe"}}
	registry := NewRegistry(builder)

	ref, err := registry.Build(context.Background(), testOptions(repoDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineStatic {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineStatic)
	}
	if builder.lastOpts.Dockerfile != staticDockerfile {
		t.Errorf("Dockerfile = %q; want %q", builder.lastOpts.Dockerfile, staticDockerfile)
	}
	files := readContextTar(t, builder.lastTar)
	if got := files["index.html"]; got != "<h1>hi</h1>\n" {
		t.Errorf("context index.html = %q", got)
	}
	if _, ok := files["assets/app.css"]; !ok {
		t.Error("context is missing assets/app.css")
	}
	if got := files[staticDockerfile]; !contains(got, staticBaseImage) {
		t.Errorf("synthesized Dockerfile does not reference %s:\n%s", staticBaseImage, got)
	}
}

func TestRegistryBuildStaticPublicDir(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "public", "index.html"), "<h1>public</h1>\n")
	writeTestFile(t, filepath.Join(repoDir, "public", "robots.txt"), "User-agent: *\n")
	writeTestFile(t, filepath.Join(repoDir, "README.md"), "docs\n")

	builder := &mockBuilder{}
	registry := NewRegistry(builder)

	ref, err := registry.Build(context.Background(), testOptions(repoDir))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineStatic {
		t.Fatalf("Kind = %q; want %q", ref.Kind, EngineStatic)
	}
	files := readContextTar(t, builder.lastTar)
	if _, ok := files["index.html"]; !ok {
		t.Error("context should be rooted at public/")
	}
	if _, ok := files["README.md"]; ok {
		t.Error("context should not include files outside public/")
	}
}

// The Railpack and Buildpacks engines shell out to CLIs that may not be
// installed on the node: with an empty PATH the build must fail with
// ErrCLIMissing and an install hint, never with a fabricated image.
func TestRegistryBuildCLIMissing(t *testing.T) {
	tests := []struct {
		name     string
		marker   string
		content  string
		wantTool string
	}{
		{name: "railpack", marker: "package.json", content: "{}\n", wantTool: "railpack"},
		{name: "buildpacks", marker: "Procfile", content: "web: ./app\n", wantTool: "pack"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			repoDir := t.TempDir()
			writeTestFile(t, filepath.Join(repoDir, tt.marker), tt.content)
			builder := &mockBuilder{}
			registry := NewRegistry(builder)

			_, err := registry.Build(context.Background(), testOptions(repoDir))
			if !errors.Is(err, ErrCLIMissing) {
				t.Fatalf("Build error = %v; want ErrCLIMissing", err)
			}
			if !strings.Contains(err.Error(), tt.wantTool) {
				t.Errorf("Build error = %q; want it to name the %s CLI", err, tt.wantTool)
			}
			if !strings.Contains(err.Error(), "install") {
				t.Errorf("Build error = %q; want an install hint", err)
			}
			if builder.calls != 0 {
				t.Errorf("builder called %d times; want 0", builder.calls)
			}
		})
	}
}

func TestRegistryBuildNoEngine(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "README.md"), "nothing to build\n")
	registry := NewRegistry(&mockBuilder{})

	_, err := registry.Build(context.Background(), testOptions(repoDir))
	if !errors.Is(err, ErrNoEngine) {
		t.Fatalf("Build error = %v; want ErrNoEngine", err)
	}
}

func TestRegistryBuildExplicitHintMismatch(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "Dockerfile"), "FROM scratch\n")
	builder := &mockBuilder{}
	registry := NewRegistry(builder)

	// An explicit hint is honoured, so the engine runs and reports its own
	// validation error instead of a generic "no engine".
	opts := testOptions(repoDir)
	opts.BuildPack = EngineStatic
	if _, err := registry.Build(context.Background(), opts); !errors.Is(err, ErrValidation) {
		t.Fatalf("Build error = %v; want ErrValidation", err)
	}
}

func TestRegistryBuildDockerfileWithoutFile(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "index.html"), "<h1>hi</h1>\n")
	registry := NewRegistry(&mockBuilder{})

	opts := testOptions(repoDir)
	opts.BuildPack = EngineDockerfile
	_, err := registry.Build(context.Background(), opts)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Build error = %v; want ErrValidation", err)
	}
}

func TestRegistryBuildValidation(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "Dockerfile"), "FROM scratch\n")
	registry := NewRegistry(&mockBuilder{})

	tests := []struct {
		name string
		opts BuildOptions
	}{
		{name: "empty repo dir", opts: BuildOptions{AppID: testAppID, DeployID: testDeployID}},
		{name: "empty app id", opts: BuildOptions{RepoDir: repoDir, DeployID: testDeployID}},
		{name: "empty deploy id", opts: BuildOptions{RepoDir: repoDir, AppID: testAppID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := registry.Build(context.Background(), tt.opts); !errors.Is(err, ErrValidation) {
				t.Fatalf("Build error = %v; want ErrValidation", err)
			}
		})
	}
}

func TestEngineWrapBuilderError(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "Dockerfile"), "FROM scratch\n")

	boom := errors.New("daemon exploded")
	builder := &mockBuilder{result: ImageBuildResult{Logs: "partial\n"}, err: boom}
	registry := NewRegistry(builder)

	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.LogWriter = &logs
	ref, err := registry.Build(context.Background(), opts)
	if !errors.Is(err, boom) {
		t.Fatalf("Build error = %v; want to wrap %v", err, boom)
	}
	if ref != (ImageRef{}) {
		t.Errorf("ImageRef = %+v; want zero value on error", ref)
	}
	if logs.String() != "partial\n" {
		t.Errorf("LogWriter = %q; want logs captured before the error", logs.String())
	}
}

func TestEngineNilBuilder(t *testing.T) {
	repoDir := t.TempDir()
	writeTestFile(t, filepath.Join(repoDir, "Dockerfile"), "FROM scratch\n")
	registry := NewRegistry(nil)

	if _, err := registry.Build(context.Background(), testOptions(repoDir)); !errors.Is(err, ErrValidation) {
		t.Fatalf("Build error = %v; want ErrValidation", err)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || bytes.Contains([]byte(haystack), []byte(needle))
}
