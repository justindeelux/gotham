package deploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateCloneURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"https", "https://github.com/acme/demo.git", true},
		{"http", "http://git.internal/acme/demo.git", true},
		{"ssh", "ssh://git@git.internal/acme/demo.git", true},
		{"scp-like", "git@git.internal:acme/demo.git", true},
		{"local path", "/srv/fixtures/demo", false},
		{"file URL", "file:///srv/fixtures/demo", false},
		{"empty", "", false},
		{"unknown scheme", "ftp://example.com/demo.git", false},
		{"option injection", "--upload-pack=touch /tmp/pwn", false},
		{"bare name", "demo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCloneURL(tc.url)
			if tc.want {
				if err != nil {
					t.Fatalf("validateCloneURL(%q) = %v, want nil", tc.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateCloneURL(%q) = nil, want an error", tc.url)
			}
			if !strings.Contains(err.Error(), ErrValidation.Error()) {
				t.Errorf("err = %v, want it wrapped in ErrValidation", err)
			}
		})
	}
}

// TestValidateCloneURLDevLocal covers the GOTHAM_DEV_CLONE_LOCAL escape
// hatch: local paths and file:// URLs are cloneable only in dev mode.
func TestValidateCloneURLDevLocal(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	for _, url := range []string{"/srv/fixtures/demo", "file:///srv/fixtures/demo"} {
		if err := validateCloneURL(url); err != nil {
			t.Errorf("validateCloneURL(%q) with %s=true = %v, want nil", url, devLocalCloneEnv, err)
		}
	}
}

func TestGitSourceRejectsInvalidURLWithoutRunningGit(t *testing.T) {
	app := testApplication(uuid.New())
	app.CloneURL = "ftp://example.com/demo.git"
	dir := filepath.Join(t.TempDir(), "repo")

	err := gitSource{}.Clone(context.Background(), app, dir, nil)
	if err == nil {
		t.Fatal("err = nil, want the clone to be refused")
	}
	if !strings.Contains(err.Error(), ErrValidation.Error()) {
		t.Errorf("err = %v, want it wrapped in ErrValidation", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("clone directory created for a refused URL (stat = %v)", statErr)
	}
}

func TestGitSourceClonesBranch(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary is not available")
	}

	origin := filepath.Join(t.TempDir(), "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatalf("mkdir origin: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = origin
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=gotham-test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=gotham-test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(origin, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "seed")
	run("branch", "-M", "main")

	app := testApplication(uuid.New())
	app.CloneURL = origin
	app.Branch = "main"
	dir := filepath.Join(t.TempDir(), "repo")

	var lines []string
	if err := (gitSource{}).Clone(context.Background(), app, dir, func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		t.Errorf("cloned tree has no Dockerfile: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "--branch main") {
		t.Errorf("log = %q, want the requested branch", joined)
	}
	if !strings.Contains(joined, "repository cloned (main)") {
		t.Errorf("log = %q, want the completion line", joined)
	}

	// A retried step re-enters Clone with a directory git already refused to
	// overwrite: the second run must clear it and succeed again.
	if err := (gitSource{}).Clone(context.Background(), app, dir, nil); err != nil {
		t.Fatalf("retried clone: %v", err)
	}
}
