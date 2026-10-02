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

// fakeCLI writes an executable shell stub named name and puts its directory at
// the front of PATH for the rest of the test, so the engines shell out to the
// stub instead of a real toolchain.
func fakeCLI(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// readFakeCLI returns the contents of a file a fake CLI wrote.
func readFakeCLI(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fake CLI output %s: %v", path, err)
	}
	return string(content)
}

// assertSameDir compares the working directory reported by a fake CLI with the
// engine's repository directory, resolving symlinks first: macOS temp
// directories are reached through /var -> /private/var, so `pwd` reports the
// physical path.
func assertSameDir(t *testing.T, got, want string) {
	t.Helper()
	resolvedGot, err := filepath.EvalSymlinks(strings.TrimSpace(got))
	if err != nil {
		t.Fatalf("resolve reported working directory %q: %v", got, err)
	}
	resolvedWant, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve repository directory %q: %v", want, err)
	}
	if resolvedGot != resolvedWant {
		t.Errorf("working directory = %q; want %q", got, want)
	}
}

// railpackMarkerRepo returns a repository the Railpack engine detects.
func railpackMarkerRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "package.json"), "{}\n")
	return dir
}

func TestRailpackBuildStreamsCLIOutput(t *testing.T) {
	repoDir := railpackMarkerRepo(t)
	out := t.TempDir()
	argsFile := filepath.Join(out, "args")
	cwdFile := filepath.Join(out, "cwd")
	fakeCLI(t, railpackCLI, fmt.Sprintf(`printf '%%s\n' "$@" > '%s'
pwd > '%s'
echo "railpack: building"
echo "railpack: start command missing" >&2
`, argsFile, cwdFile))
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	builder := &mockBuilder{}
	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.LogWriter = &logs
	opts.BuildArgs = map[string]string{"GO_VERSION": "1.22", "A_FLAG": "1"}

	ref, err := NewRegistry(nil).Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ref.Kind != EngineRailpack {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineRailpack)
	}
	if want := ImageTag(testAppID, testDeployID); ref.Tag != want {
		t.Errorf("Tag = %q; want %q", ref.Tag, want)
	}
	if builder.calls != 0 {
		t.Errorf("ImageBuilder called %d times; want 0", builder.calls)
	}

	log := logs.String()
	for _, want := range []string{"railpack: building", "railpack: start command missing", "$ railpack build"} {
		if !strings.Contains(log, want) {
			t.Errorf("LogWriter = %q; want it to contain %q", log, want)
		}
	}

	args := strings.Split(strings.TrimSuffix(readFakeCLI(t, argsFile), "\n"), "\n")
	wantArgs := []string{
		"build", "--name", ref.Tag, "--progress", "plain",
		"--env", "A_FLAG=1", "--env", "GO_VERSION=1.22",
		".",
	}
	if !slices.Equal(args, wantArgs) {
		t.Errorf("railpack args = %q; want %q", args, wantArgs)
	}
	assertSameDir(t, readFakeCLI(t, cwdFile), repoDir)
}

func TestRailpackBuildRequiresBuildKit(t *testing.T) {
	repoDir := railpackMarkerRepo(t)
	ranFile := filepath.Join(t.TempDir(), "ran")
	fakeCLI(t, railpackCLI, fmt.Sprintf(`echo ran > '%s'`, ranFile))
	t.Setenv("BUILDKIT_HOST", "")

	builder := &mockBuilder{}
	_, err := NewRegistry(nil).Build(context.Background(), testOptions(repoDir))
	if !errors.Is(err, ErrCLIMissing) {
		t.Fatalf("Build error = %v; want ErrCLIMissing", err)
	}
	if !strings.Contains(err.Error(), "moby/buildkit") {
		t.Errorf("Build error = %q; want the BuildKit start command", err)
	}
	if _, statErr := os.Stat(ranFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("railpack ran although BUILDKIT_HOST was unset")
	}
	if builder.calls != 0 {
		t.Errorf("ImageBuilder called %d times; want 0", builder.calls)
	}
}

func TestRailpackBuildCLIError(t *testing.T) {
	repoDir := railpackMarkerRepo(t)
	fakeCLI(t, railpackCLI, `echo "no start command found" >&2
exit 1`)
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.LogWriter = &logs

	ref, err := NewRailpackEngine(nil).Build(context.Background(), opts)
	if err == nil {
		t.Fatal("Build succeeded although the CLI failed")
	}
	if errors.Is(err, ErrCLIMissing) {
		t.Errorf("Build error = %v; a failing CLI is not a missing toolchain", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("Build error = %v; want the CLI exit status", err)
	}
	if !strings.Contains(logs.String(), "no start command found") {
		t.Errorf("LogWriter = %q; want the CLI's failure reason", logs.String())
	}
	if ref != (ImageRef{}) {
		t.Errorf("ImageRef = %+v; want the zero value on error", ref)
	}
}

func TestRailpackBuildContextCancelled(t *testing.T) {
	repoDir := railpackMarkerRepo(t)
	out := t.TempDir()
	startedFile := filepath.Join(out, "started")
	fakeCLI(t, railpackCLI, fmt.Sprintf(`echo building
echo started > '%s'
while :; do :; done`, startedFile))
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	// The stub stays inside the shell: a `sleep` grandchild would inherit the
	// output pipe and keep the build from stopping when the context is done.
	var logs bytes.Buffer
	opts := testOptions(repoDir)
	opts.LogWriter = &logs

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := NewRailpackEngine(nil).Build(ctx, opts)
		done <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake railpack never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Build error = %v; want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Build kept running after the context was cancelled")
	}
	if !strings.Contains(logs.String(), "building") {
		t.Errorf("LogWriter = %q; want the output streamed before cancellation", logs.String())
	}
}

// TestRailpackEngineE2E builds a small Go application with a real railpack
// installation against the local Docker daemon.
func TestRailpackEngineE2E(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run live builds")
	}
	if _, err := exec.LookPath(railpackCLI); err != nil {
		t.Skipf("%s is not on PATH: %v", railpackCLI, err)
	}
	if strings.TrimSpace(os.Getenv("BUILDKIT_HOST")) == "" {
		t.Skip("BUILDKIT_HOST is not set; start a BuildKit daemon first")
	}
	// railpack pipes its image into the local daemon (`docker load`), so the
	// daemon has to be reachable even though this engine never calls the
	// ImageBuilder seam.
	requireDocker(t)

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/gotham-e2e\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var logs bytes.Buffer
	opts := testOptions(dir)
	opts.LogWriter = &logs

	ref, err := NewRegistry(nil).Build(ctx, opts)
	if err != nil {
		t.Fatalf("Build: %v (logs: %s)", err, logs.String())
	}
	if ref.Kind != EngineRailpack {
		t.Errorf("Kind = %q; want %q", ref.Kind, EngineRailpack)
	}
	if want := ImageTag(testAppID, testDeployID); ref.Tag != want {
		t.Errorf("Tag = %q; want %q", ref.Tag, want)
	}
	if logs.Len() == 0 {
		t.Error("Build streamed no logs")
	}
}
