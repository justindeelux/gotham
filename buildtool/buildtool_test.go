package buildtool

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// writeTar builds an uncompressed tar from name -> contents.
func writeTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func TestExtractTar(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractTar(dir, bytes.NewReader(writeTar(t, map[string]string{
		"app/main.go": "package main\n",
		"Procfile":    "web: ./app\n",
	}))); err != nil {
		t.Fatalf("extract: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "app", "main.go"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(content) != "package main\n" {
		t.Errorf("content = %q", content)
	}
}

func TestExtractTarRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "/abs/path", "a/../../escape"} {
		dir := t.TempDir()
		err := ExtractTar(dir, bytes.NewReader(writeTar(t, map[string]string{name: "x"})))
		if err == nil {
			t.Errorf("ExtractTar(%q) succeeded; want an escape error", name)
		}
	}
}

func TestAvailableMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, engine := range []Engine{Railpack, Buildpacks} {
		if err := Available(engine); !errors.Is(err, ErrCLIMissing) {
			t.Errorf("Available(%q) = %v; want ErrCLIMissing", engine, err)
		}
	}
}

func TestRunStreamsOutputAndFlags(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	fakeCLI(t, RailpackCLI, fmt.Sprintf(`printf '%%s\n' "$@" > '%s'
echo building`, argsFile))
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	var logs bytes.Buffer
	if err := Run(context.Background(), Railpack, Options{
		Dir:       dir,
		Tag:       "gotham/app:dep",
		BuildArgs: map[string]string{"B": "2", "A": "1"},
		LogWriter: &logs,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(readFile(t, argsFile), "\n"), "\n")
	want := []string{"build", "--name", "gotham/app:dep", "--progress", "plain", "--env", "A=1", "--env", "B=2", "."}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %q; want %q", args, want)
	}
	if !strings.Contains(logs.String(), "building") || !strings.Contains(logs.String(), "$ railpack build") {
		t.Errorf("logs = %q", logs.String())
	}
}

// TestRunStripsInheritedSecrets checks the toolchain runs with a minimal
// environment: a parent secret must not leak, while the toolchain's own inputs
// (Docker/BuildKit addresses) are passed through.
func TestRunStripsInheritedSecrets(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	fakeCLI(t, RailpackCLI, fmt.Sprintf(`env > '%s'`, envFile))
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")
	t.Setenv("GOTHAM_SECRET_SENTINEL", "leak-me")

	if err := Run(context.Background(), Railpack, Options{
		Dir:        dir,
		Tag:        "gotham/app:dep",
		DockerHost: "unix:///var/run/docker.sock",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	env := readFile(t, envFile)
	if strings.Contains(env, "GOTHAM_SECRET_SENTINEL") {
		t.Error("toolchain inherited a parent secret")
	}
	if !strings.Contains(env, "DOCKER_HOST=unix:///var/run/docker.sock") {
		t.Errorf("child env missing DOCKER_HOST:\n%s", env)
	}
	if !strings.Contains(env, "BUILDKIT_HOST=docker-container://buildkit") {
		t.Errorf("child env missing BUILDKIT_HOST:\n%s", env)
	}
}

// TestRunStartsBuildKitWhenUnset checks Railpack needs no manual BuildKit setup:
// with BUILDKIT_HOST unset, Run creates the container through docker and
// points Railpack at it.
func TestRunStartsBuildKitWhenUnset(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	dockerLog := filepath.Join(dir, "docker")
	fakeCLI(t, RailpackCLI, fmt.Sprintf(`env > '%s'`, envFile))
	// inspect reports no container yet, so Run must `docker run` it and wait for exec.
	fakeCLI(t, "docker", fmt.Sprintf(`echo "$@" >> '%s'; if [ "$1" = inspect ]; then echo "Error: No such object: buildkit"; exit 1; fi`, dockerLog))
	t.Setenv("BUILDKIT_HOST", "")

	if err := Run(context.Background(), Railpack, Options{Dir: dir, Tag: "gotham/app:dep"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readFile(t, dockerLog); !strings.Contains(got, "run -d --privileged --restart unless-stopped --name buildkit moby/buildkit") {
		t.Errorf("docker calls = %q; want a buildkit run", got)
	}
	if env := readFile(t, envFile); !strings.Contains(env, "BUILDKIT_HOST=docker-container://buildkit") {
		t.Errorf("child env missing BUILDKIT_HOST:\n%s", env)
	}
}

// fakeCLI writes an executable shell stub named name and prepends its directory
// to PATH for the test.
func fakeCLI(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// TestRunRedactsBuildArgValues is the C1-13 regression: the logged command line
// must not expose build-arg values (they may be secrets).
func TestRunRedactsBuildArgValues(t *testing.T) {
	dir := t.TempDir()
	fakeCLI(t, RailpackCLI, "echo building")
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	var logs bytes.Buffer
	if err := Run(context.Background(), Railpack, Options{
		Dir:       dir,
		Tag:       "gotham/app:dep",
		BuildArgs: map[string]string{"API_TOKEN": "s3cr3t-value"},
		LogWriter: &logs,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(logs.String(), "s3cr3t-value") {
		t.Fatalf("deploy log leaked the build-arg value: %q", logs.String())
	}
	if !strings.Contains(logs.String(), "API_TOKEN=***") {
		t.Errorf("deploy log does not show the redacted key: %q", logs.String())
	}
}

// TestRunFailureIncludesOutputTail is the JUS-81 regression: a failing
// toolchain must report the last lines of its output in the error, so the
// deploy row keeps the cause after the live log is gone.
func TestRunFailureIncludesOutputTail(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&body, "echo 'line %02d'\n", i)
	}
	body.WriteString("printf '\\033[31mERROR pnpm install failed\\033[0m\\n'\n")
	body.WriteString("echo 'API_TOKEN=s3cr3t-value'\n")
	body.WriteString("exit 1\n")
	fakeCLI(t, RailpackCLI, body.String())
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	var logs bytes.Buffer
	err := Run(context.Background(), Railpack, Options{
		Dir:       dir,
		Tag:       "gotham/app:dep",
		BuildArgs: map[string]string{"API_TOKEN": "s3cr3t-value"},
		LogWriter: &logs,
	})
	if err == nil {
		t.Fatal("Run succeeded; want the toolchain failure")
	}
	if errors.Is(err, ErrCLIMissing) {
		t.Fatalf("Run error = %v; a failing CLI is not a missing toolchain", err)
	}
	message := err.Error()
	if !strings.HasPrefix(message, "railpack build: ") {
		t.Errorf("error = %q; want the existing `railpack build:` prefix", message)
	}
	marker := strings.Index(message, "--- last output ---")
	if marker < 0 {
		t.Fatalf("error = %q; want the `--- last output ---` section", message)
	}
	section := message[marker:]
	for _, want := range []string{"line 30", "ERROR pnpm install failed", "API_TOKEN=***"} {
		if !strings.Contains(section, want) {
			t.Errorf("tail = %q; want it to contain %q", section, want)
		}
	}
	if strings.Contains(section, "line 01") {
		t.Errorf("tail = %q; want only the last lines kept", section)
	}
	if strings.Contains(message, "\x1b[") {
		t.Errorf("error = %q; want ANSI escape codes stripped", message)
	}
	if strings.Contains(message, "s3cr3t-value") {
		t.Errorf("error = %q; want the build-arg value masked", message)
	}
	if len(section) > tailMaxBytes+len("--- last output ---\n")+1 {
		t.Errorf("tail section is %d bytes; want it bounded near %d", len(section), tailMaxBytes)
	}
}

// TestRunFailureWithoutOutput keeps the plain error when the tool fails
// silently: no tail section is appended to an empty tail.
func TestRunFailureWithoutOutput(t *testing.T) {
	dir := t.TempDir()
	fakeCLI(t, RailpackCLI, "exit 1")
	t.Setenv("BUILDKIT_HOST", "docker-container://buildkit")

	err := Run(context.Background(), Railpack, Options{Dir: dir, Tag: "gotham/app:dep"})
	if err == nil {
		t.Fatal("Run succeeded; want the toolchain failure")
	}
	if strings.Contains(err.Error(), "--- last output ---") {
		t.Errorf("error = %q; want no tail section for empty output", err)
	}
}

// TestOutputTailOverlappingSecrets is the round-2 finding 1 regression: a
// shorter value prefixing a longer one must not leak the longer one's suffix.
func TestOutputTailOverlappingSecrets(t *testing.T) {
	tail := newOutputTail([]string{"abc", "abcdef"})
	_, _ = tail.Write([]byte("key=abcdef\n"))
	if got := tail.String(); got != "key=***" {
		t.Errorf("tail = %q; want the longer value masked first", got)
	}
}

// TestOutputTailMultilineSecret is the round-2 finding 2 regression: each
// non-empty line of a PEM-like value is masked even though the full value
// never appears on one output line.
func TestOutputTailMultilineSecret(t *testing.T) {
	tail := newOutputTail([]string{"-----BEGIN KEY-----\nsecret-body\n-----END KEY-----"})
	_, _ = tail.Write([]byte("-----BEGIN KEY-----\nleaked secret-body here\n-----END KEY-----\n"))
	got := tail.String()
	for _, leaked := range []string{"BEGIN KEY", "secret-body", "END KEY"} {
		if strings.Contains(got, leaked) {
			t.Errorf("tail = %q; want no line of the multi-line secret", got)
		}
	}
}

// TestOutputTailOversizedSingleLine is the round-2 finding 4 regression: one
// line over the cap keeps its end instead of emptying the tail.
func TestOutputTailOversizedSingleLine(t *testing.T) {
	tail := newOutputTail(nil)
	_, _ = tail.Write([]byte(strings.Repeat("a", tailMaxBytes+100) + "CAUSE\n"))
	got := tail.String()
	if got == "" {
		t.Fatal("tail is empty; want the newest line kept truncated")
	}
	if !strings.HasSuffix(got, "CAUSE") {
		t.Errorf("tail = %q; want the line end kept", got)
	}
	if len(got) > tailMaxBytes {
		t.Errorf("tail is %d bytes; want it within %d", len(got), tailMaxBytes)
	}
}

// TestOutputTailTrailingPartial keeps output without a trailing newline.
func TestOutputTailTrailingPartial(t *testing.T) {
	tail := newOutputTail(nil)
	_, _ = tail.Write([]byte("cause without newline"))
	if got := tail.String(); got != "cause without newline" {
		t.Errorf("tail = %q; want the partial line flushed", got)
	}
}

// TestOutputTailCarriageReturn is the round-2 finding 5 regression: \r ends a
// line so progress bars that overwrite in place still advance the tail.
func TestOutputTailCarriageReturn(t *testing.T) {
	tail := newOutputTail(nil)
	_, _ = tail.Write([]byte("10%\r20%\r30%\n"))
	if got := tail.String(); got != "10%\n20%\n30%" {
		t.Errorf("tail = %q; want \\r to split lines", got)
	}
}

// TestOutputTailPendingBounded is the round-2 finding 5 regression: a
// newline-less flood cannot grow the pending buffer without limit, and the
// newest bytes still survive.
func TestOutputTailPendingBounded(t *testing.T) {
	tail := newOutputTail(nil)
	_, _ = tail.Write([]byte(strings.Repeat("x", pendingMaxBytes+1000)))
	if tail.pending.Len() > pendingMaxBytes {
		t.Errorf("pending is %d bytes; want it capped at %d", tail.pending.Len(), pendingMaxBytes)
	}
	_, _ = tail.Write([]byte("end\n"))
	if got := tail.String(); !strings.HasSuffix(got, "end") {
		t.Errorf("tail = %q; want the newest bytes kept", got)
	}
}

// TestFailureTailFitsTruncateLimit is the round-2 finding 3 regression: a full
// tail plus the run error prefix stays within truncateError's 4 KiB limit and
// stays valid UTF-8, so the cause lines survive persistence.
func TestFailureTailFitsTruncateLimit(t *testing.T) {
	tail := newOutputTail(nil)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(tail, "line %d %s\n", i, strings.Repeat("é", 50))
	}
	message := "railpack build: exit status 1\n--- last output ---\n" + tail.String()
	if len(message) > 4096 {
		t.Errorf("message is %d bytes; want prefix+tail within 4096", len(message))
	}
	if !utf8.ValidString(message) {
		t.Error("message is not valid UTF-8")
	}
	if !strings.Contains(message, "line 99") {
		t.Errorf("message = %q; want the newest lines kept", message)
	}
}

func TestRedactedCommandLine(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "separate flag",
			args: []string{"build", "--env", "TOKEN=abc", "."},
			want: "$ pack build --env TOKEN=*** .",
		},
		{
			name: "inline flag",
			args: []string{"build", "--env=TOKEN=abc"},
			want: "$ pack build --env=TOKEN=***",
		},
		{
			name: "non build-arg keeps value",
			args: []string{"build", "--name", "gotham/app:dep"},
			want: "$ pack build --name gotham/app:dep",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactedCommandLine("pack", tt.args); got != tt.want {
				t.Errorf("redactedCommandLine = %q; want %q", got, tt.want)
			}
		})
	}
}
