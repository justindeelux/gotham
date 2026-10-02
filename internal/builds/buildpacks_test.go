package builds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// buildpacksMarkerRepo returns a repository the Buildpacks engine detects. The
// explicit BuildPack hint matters: go.mod also matches Railpack, which wins the
// auto-detection order.
func buildpacksMarkerRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/gotham-e2e\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "Procfile"), "web: ./gotham-e2e\n")
	return dir
}

func TestBuildpacksBuildStreamsCLIOutput(t *testing.T) {
	repoDir := buildpacksMarkerRepo(t)
	out := t.TempDir()
	argsFile := filepath.Join(out, "args")
	cwdFile := filepath.Join(out, "cwd")
	fakeCLI(t, packCLI, fmt.Sprintf(`printf '%%s\n' "$@" > '%s'
pwd > '%s'
echo "[builder] detected go"
echo "[builder] no compatible buildpacks" >&2
`, argsFile, cwdFile))

	builder := &mockBuilder{}
	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.BuildPack = EngineBuildpacks
	opts.LogWriter = &logs
	opts.BuildArgs = map[string]string{"GOPRIVATE": "example.com"}

	ref, err := NewRegistry(nil).Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineBuildpacks {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineBuildpacks)
	}
	if want := ImageTag(testAppID, testDeployID); ref.Tag != want {
		t.Errorf("Tag = %q; want %q", ref.Tag, want)
	}
	if builder.calls != 0 {
		t.Errorf("ImageBuilder called %d times; want 0", builder.calls)
	}

	log := logs.String()
	for _, want := range []string{"[builder] detected go", "[builder] no compatible buildpacks", "$ pack build"} {
		if !strings.Contains(log, want) {
			t.Errorf("LogWriter = %q; want it to contain %q", log, want)
		}
	}

	args := strings.Split(strings.TrimSuffix(readFakeCLI(t, argsFile), "\n"), "\n")
	wantArgs := []string{"build", ref.Tag, "--path", ".", "--env", "GOPRIVATE=example.com"}
	if !slices.Equal(args, wantArgs) {
		t.Errorf("pack args = %q; want %q", args, wantArgs)
	}
	assertSameDir(t, readFakeCLI(t, cwdFile), repoDir)
}

func TestBuildpacksBuildCLIMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	repoDir := buildpacksMarkerRepo(t)

	_, err := NewBuildpacksEngine(nil).Build(context.Background(), testOptions(repoDir))
	if !errors.Is(err, ErrCLIMissing) {
		t.Fatalf("Build error = %v; want ErrCLIMissing", err)
	}
	if !strings.Contains(err.Error(), "brew install buildpacks/pack/pack") {
		t.Errorf("Build error = %q; want the pack install command", err)
	}
}

func TestBuildpacksBuildCLIError(t *testing.T) {
	repoDir := buildpacksMarkerRepo(t)
	fakeCLI(t, packCLI, `echo "Failed to build: no builder image" >&2
exit 1`)

	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.BuildPack = EngineBuildpacks
	opts.LogWriter = &logs

	ref, err := NewBuildpacksEngine(nil).Build(context.Background(), opts)
	if err == nil {
		t.Fatal("Build succeeded although the CLI failed")
	}
	if errors.Is(err, ErrCLIMissing) {
		t.Errorf("Build error = %v; a failing CLI is not a missing toolchain", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("Build error = %v; want the CLI exit status", err)
	}
	if !strings.Contains(logs.String(), "no builder image") {
		t.Errorf("LogWriter = %q; want the CLI's failure reason", logs.String())
	}
	if ref != (ImageRef{}) {
		t.Errorf("ImageRef = %+v; want the zero value on error", ref)
	}
}

// TestBuildpacksEngineE2E builds a small Go application with a real pack
// installation against the local Docker daemon. The fixture pins a public
// builder in project.toml so the run does not depend on the pack default
// builder configured on this machine.
func TestBuildpacksEngineE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live builds")
	}
	if _, err := exec.LookPath(packCLI); err != nil {
		t.Skipf("%s is not on PATH: %v", packCLI, err)
	}
	// pack drives the local daemon for detect, build and export.
	requireDocker(t)

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/gotham-e2e\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(dir, "project.toml"), "[project]\n"+
		"id = \"gotham-e2e\"\nname = \"gotham e2e\"\nversion = \"0.0.1\"\n\n"+
		"[build]\nbuilder = \"paketobuildpacks/builder-jammy-base\"\n")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var logs bytes.Buffer
	opts := testOptions(dir)
	opts.BuildPack = EngineBuildpacks
	opts.LogWriter = &logs

	ref, err := NewRegistry(nil).Build(ctx, opts)
	if err != nil {
		t.Fatalf("Build: %v (logs: %s)", err, logs.String())
	}
	if ref.Kind != EngineBuildpacks {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineBuildpacks)
	}
	if want := ImageTag(testAppID, testDeployID); ref.Tag != want {
		t.Errorf("Tag = %q; want %q", ref.Tag, want)
	}
	if logs.Len() == 0 {
		t.Error("Build streamed no logs")
	}
}
