package deploy

import (
	"context"
	"errors"
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

// staticKeyResolver hands out a fixed answer for the deploy-key lookup.
type staticKeyResolver struct {
	pem   string
	err   error
	calls int
}

// Compile-time guarantee that staticKeyResolver satisfies the seam.
var _ deployKeyResolver = (*staticKeyResolver)(nil)

// DeployKeyPrivatePEM implements deployKeyResolver.
func (r *staticKeyResolver) DeployKeyPrivatePEM(_ context.Context, appID uuid.UUID) (string, error) {
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	return r.pem, nil
}

// sshCommandKeyPath extracts the `-i` key path from a captured GIT_SSH_COMMAND.
func sshCommandKeyPath(env []string) string {
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
			continue
		}
		command := strings.TrimPrefix(entry, "GIT_SSH_COMMAND=")
		_, rest, ok := strings.Cut(command, "-i '")
		if !ok {
			return ""
		}
		path, _, ok := strings.Cut(rest, "'")
		if !ok {
			return ""
		}
		return path
	}
	return ""
}

// hasEntry reports whether env carries a variable with the given name.
func hasEntry(env []string, name string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, name+"=") {
			return true
		}
	}
	return false
}

// TestGitSourceCloneWithDeployKey asserts everything the keyed clone must do:
// rewrite the URL to SSH, point GIT_SSH_COMMAND at a 0600 ephemeral key file,
// keep that file alive for the run only, and never log key material. The git
// invocation itself is replaced, so no network and no sshd are involved.
func TestGitSourceCloneWithDeployKey(t *testing.T) {
	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	resolver := &staticKeyResolver{pem: privatePEM}
	app := testApplication(uuid.New())
	dir := filepath.Join(t.TempDir(), "repo")

	var gotArgv, gotEnv []string
	var keyPath string
	run := func(_ context.Context, argv, env []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), env...)

		keyPath = sshCommandKeyPath(env)
		if keyPath == "" {
			t.Error("GIT_SSH_COMMAND carries no -i key path")
			return nil, nil
		}
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Errorf("stat deploy key: %v", err)
			return nil, nil
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("deploy key mode = %o, want 600", perm)
		}
		stored, err := os.ReadFile(keyPath)
		if err != nil {
			t.Errorf("read deploy key: %v", err)
			return nil, nil
		}
		// The cloner trims surrounding whitespace before materialising the
		// key, so the comparison is on the trimmed PEM.
		if strings.TrimSpace(string(stored)) != strings.TrimSpace(privatePEM) {
			t.Error("the ephemeral file does not hold the application's private key")
		}
		return nil, nil
	}

	var lines []string
	source := gitSource{keys: resolver, run: run}
	if err := source.Clone(context.Background(), app, dir, func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if resolver.calls != 1 {
		t.Errorf("key lookups = %d, want 1", resolver.calls)
	}

	if len(gotArgv) == 0 || gotArgv[0] != "git" {
		t.Fatalf("argv = %v, want a git command", gotArgv)
	}
	joinedArgv := strings.Join(gotArgv, " ")
	if !strings.Contains(joinedArgv, "ssh://git@github.com/acme/demo.git") {
		t.Errorf("argv = %v, want the clone URL rewritten to SSH", gotArgv)
	}
	if strings.Contains(joinedArgv, "https://") {
		t.Errorf("argv = %v, want no anonymous https URL next to a deploy key", gotArgv)
	}

	command := ""
	for _, entry := range gotEnv {
		if strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
			command = strings.TrimPrefix(entry, "GIT_SSH_COMMAND=")
		}
	}
	for _, option := range []string{"-i '", "IdentitiesOnly=yes", "StrictHostKeyChecking=accept-new", "UserKnownHostsFile="} {
		if !strings.Contains(command, option) {
			t.Errorf("GIT_SSH_COMMAND = %q, want it to contain %q", command, option)
		}
	}

	if keyPath == "" {
		t.Fatal("the runner never saw a key path")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Errorf("deploy key survived the clone (stat = %v), want it removed", err)
	}

	joinedLog := strings.Join(lines, "\n")
	if !strings.Contains(joinedLog, "(using the application deploy key)") {
		t.Errorf("log = %q, want the deploy-key marker", joinedLog)
	}
	if strings.Contains(joinedLog, "BEGIN OPENSSH") || strings.Contains(joinedLog, strings.Split(privatePEM, "\n")[1]) {
		t.Error("the deploy log leaked key material")
	}
}

// TestGitSourceCloneWithoutDeployKeyStaysAnonymous pins the default: with no
// key in the database (or no resolver wired at all) the clone is the anonymous
// https one it has always been, with no GIT_SSH_COMMAND in the environment.
func TestGitSourceCloneWithoutDeployKeyStaysAnonymous(t *testing.T) {
	app := testApplication(uuid.New()) // https://github.com/acme/demo.git
	dir := filepath.Join(t.TempDir(), "repo")

	var gotArgv, gotEnv []string
	run := func(_ context.Context, argv, env []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), env...)
		return nil, nil
	}

	cases := []struct {
		name   string
		source gitSource
	}{
		{"keyless application", gitSource{keys: &staticKeyResolver{}, run: run}},
		{"no resolver", gitSource{run: run}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotArgv, gotEnv = nil, nil
			if err := tc.source.Clone(context.Background(), app, dir, nil); err != nil {
				t.Fatalf("Clone: %v", err)
			}
			joinedArgv := strings.Join(gotArgv, " ")
			if !strings.Contains(joinedArgv, app.CloneURL) {
				t.Errorf("argv = %v, want the unchanged clone URL", gotArgv)
			}
			if hasEntry(gotEnv, "GIT_SSH_COMMAND") {
				t.Errorf("env = %v, want no GIT_SSH_COMMAND for an anonymous clone", gotEnv)
			}
		})
	}
}

// TestGitSourceCloneKeyLookupFailureStopsClone makes a broken key lookup loud
// instead of silently degrading to an anonymous clone the host would reject.
func TestGitSourceCloneKeyLookupFailureStopsClone(t *testing.T) {
	app := testApplication(uuid.New())
	dir := filepath.Join(t.TempDir(), "repo")
	ran := false
	source := gitSource{
		keys: &staticKeyResolver{err: errors.New("database down")},
		run: func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		},
	}

	err := source.Clone(context.Background(), app, dir, nil)
	if err == nil {
		t.Fatal("Clone succeeded although the deploy key could not be opened")
	}
	if !strings.Contains(err.Error(), "database down") {
		t.Errorf("error = %v, want the lookup failure surfaced", err)
	}
	if ran {
		t.Error("git ran although the deploy key lookup failed")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("clone directory created for a failed lookup (stat = %v)", statErr)
	}
}

// TestSSHCloneURL covers the http(s) → ssh rewrite a deploy key needs, and the
// URL shapes that must pass through untouched.
func TestSSHCloneURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/acme/demo.git", "ssh://git@github.com/acme/demo.git"},
		{"http://git.internal/acme/demo.git", "ssh://git@git.internal/acme/demo.git"},
		{"https://gitea.example:3000/acme/demo.git", "ssh://git@gitea.example/acme/demo.git"},
		{"ssh://git@git.internal/acme/demo.git", "ssh://git@git.internal/acme/demo.git"},
		{"git@git.internal:acme/demo.git", "git@git.internal:acme/demo.git"},
		{"/srv/fixtures/demo", "/srv/fixtures/demo"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := sshCloneURL(tc.in); got != tc.want {
				t.Errorf("sshCloneURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
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
