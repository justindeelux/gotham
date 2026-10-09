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
	// inspect fails (no container yet), so Run must `docker run` it.
	fakeCLI(t, "docker", fmt.Sprintf(`echo "$@" >> '%s'; [ "$1" != inspect ]`, dockerLog))
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
