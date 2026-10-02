package deploy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
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
		{"ssh with port", "ssh://git@git.internal:2222/acme/demo.git", true},
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

// sshCommandKnownHostsPath extracts the UserKnownHostsFile path from a captured
// GIT_SSH_COMMAND.
func sshCommandKnownHostsPath(env []string) string {
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
			continue
		}
		command := strings.TrimPrefix(entry, "GIT_SSH_COMMAND=")
		_, rest, ok := strings.Cut(command, "UserKnownHostsFile='")
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

// countEntry counts env entries naming the variable. Git uses the last one it
// sees, so two GIT_SSH_COMMAND entries are a real ambiguity, not a harmless
// duplicate.
func countEntry(env []string, name string) int {
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, name+"=") {
			count++
		}
	}
	return count
}

// ambient env used by the clone tests to prove a CI-runner/developer
// GIT_SSH_COMMAND and unrelated variables are handled deterministically.
const (
	ambientSSHCommand = "ssh -o ControlMaster=no -o BatchMode=yes"
	ambientEnvName    = "GOTHAM_CLONER_TEST_AMBIENT"
	ambientEnvValue   = "survives"
)

// TestGitSourceCloneWithDeployKey asserts everything the keyed clone must do:
// rewrite the URL to SSH, point GIT_SSH_COMMAND at a 0600 ephemeral key file,
// keep that file alive for the run only, and never log key material. The git
// invocation itself is replaced, so no network and no sshd are involved.
func TestGitSourceCloneWithDeployKey(t *testing.T) {
	// A CI runner (actions/checkout) or a developer shell may export
	// GIT_SSH_COMMAND. It must not survive into the keyed clone: the child
	// gets exactly the cloner-constructed command.
	t.Setenv("GIT_SSH_COMMAND", ambientSSHCommand)
	t.Setenv(ambientEnvName, ambientEnvValue)

	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	resolver := &staticKeyResolver{pem: privatePEM}
	app := testApplication(uuid.New())
	dir := filepath.Join(t.TempDir(), "repo")

	var gotArgv, gotEnv []string
	var keyPath, knownHostsPath string
	var knownHostsContent []byte
	run := func(_ context.Context, argv, env []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), env...)

		// The ephemeral directory is removed as soon as Clone returns, so the
		// known_hosts is captured here rather than after the call.
		knownHostsPath = sshCommandKnownHostsPath(env)
		if knownHostsPath != "" {
			if data, readErr := os.ReadFile(knownHostsPath); readErr == nil {
				knownHostsContent = data
			} else {
				t.Errorf("read known_hosts: %v", readErr)
			}
		}

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

	if got := countEntry(gotEnv, "GIT_SSH_COMMAND"); got != 1 {
		t.Fatalf("GIT_SSH_COMMAND entries = %d in env %v, want exactly 1", got, gotEnv)
	}
	command := ""
	for _, entry := range gotEnv {
		if strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
			command = strings.TrimPrefix(entry, "GIT_SSH_COMMAND=")
		}
	}
	for _, option := range []string{"-i '", "IdentitiesOnly=yes", "StrictHostKeyChecking=yes", "UserKnownHostsFile="} {
		if !strings.Contains(command, option) {
			t.Errorf("GIT_SSH_COMMAND = %q, want it to contain %q", command, option)
		}
	}
	if keyPath != "" && !strings.Contains(command, "-i '"+keyPath+"'") {
		t.Errorf("GIT_SSH_COMMAND = %q, want it to carry -i %q", command, keyPath)
	}
	// The pinned provider keys must be present in the known_hosts the clone
	// verifies against, so github.com/gitlab.com work without operator setup.
	if knownHostsPath == "" {
		t.Fatal("GIT_SSH_COMMAND carries no UserKnownHostsFile path")
	}
	for _, host := range []string{"github.com", "gitlab.com"} {
		if !strings.Contains(string(knownHostsContent), host+" ssh-ed25519 ") {
			t.Errorf("known_hosts is missing the pinned %s key:\n%s", host, knownHostsContent)
		}
	}
	if !hasEntry(gotEnv, ambientEnvName) {
		t.Errorf("env = %v, want the ambient %s preserved", gotEnv, ambientEnvName)
	}
	if strings.Contains(command, ambientSSHCommand) {
		t.Errorf("GIT_SSH_COMMAND = %q, want the ambient command stripped", command)
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
	// The anonymous clone must drop an inherited GIT_SSH_COMMAND rather than
	// pass it through (the Dependabot-CI failure), while keeping the rest of
	// the ambient environment.
	t.Setenv("GIT_SSH_COMMAND", ambientSSHCommand)
	t.Setenv(ambientEnvName, ambientEnvValue)

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
			if got := countEntry(gotEnv, "GIT_SSH_COMMAND"); got != 0 {
				t.Errorf("GIT_SSH_COMMAND entries = %d in env %v, want 0", got, gotEnv)
			}
			if !hasEntry(gotEnv, ambientEnvName) {
				t.Errorf("env = %v, want the ambient %s preserved", gotEnv, ambientEnvName)
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
// URL shapes that must pass through untouched. The FE stores the provider's
// own ssh_url for a private repository (BE-4.4b), so an ssh URL — including a
// self-hosted instance on a non-default port — is what the cloner normally
// sees and must never be rewritten.
func TestSSHCloneURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/acme/demo.git", "ssh://git@github.com/acme/demo.git"},
		{"http://git.internal/acme/demo.git", "ssh://git@git.internal/acme/demo.git"},
		{"https://gitea.example:3000/acme/demo.git", "ssh://git@gitea.example/acme/demo.git"},
		{"ssh://git@git.internal/acme/demo.git", "ssh://git@git.internal/acme/demo.git"},
		{"ssh://git@git.internal:2222/acme/demo.git", "ssh://git@git.internal:2222/acme/demo.git"},
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

// seedGitFixture creates a local git repository with one commit on main and
// returns its directory. It skips the test when git is unavailable.
func seedGitFixture(t *testing.T) string {
	t.Helper()
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
	return origin
}

func TestGitSourceClonesBranch(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	origin := seedGitFixture(t)

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

// stubSSHScript is a test stand-in for ssh: it records the arguments and the
// deploy key the cloner handed it, then serves the repository with
// git-upload-pack locally. The last argument is the remote command git asked
// ssh to run, so the stub closes the ssh transport over local pipes — no
// network, no sshd, no host keys.
const stubSSHScript = `#!/bin/sh
record="$GOTHAM_TEST_SSH_RECORD"
key=""
prev=""
for arg in "$@"; do
  echo "arg: $arg" >> "$record"
  if [ "$prev" = "-i" ]; then key="$arg"; fi
  prev="$arg"
done
if [ -n "$key" ] && [ -f "$key" ]; then
  cat "$key" > "$GOTHAM_TEST_SSH_KEY_COPY"
  # GNU stat and BSD stat disagree on the flag: GNU -c %a, BSD -f %Lp. Probe
  # GNU first — the BSD form is accepted by GNU stat as a filesystem query
  # that prints a multi-line dump and exits 0, so a BSD-first probe never
  # reaches its fallback and records no single-token mode on Linux CI.
  mode=$(stat -c %a "$key" 2>/dev/null || stat -f %Lp "$key" 2>/dev/null | head -n1)
  printf 'mode: %s\n' "$mode" >> "$record"
fi
for last in "$@"; do :; done
exec /bin/sh -c "$last"
`

// TestGitSourceClonesPrivateRepoOverSSH exercises the private-repository clone
// path end to end without a network: a real git binary clones a real fixture
// repository through the GIT_SSH_COMMAND the cloner builds, with a stub `ssh`
// on PATH that checks the ephemeral deploy key and serves the repository with
// git-upload-pack. It covers the pieces BE-4.4b depends on — the stored SSH
// clone URL, the 0600 key file, the ssh options and git's ssh transport — but
// not SSH itself: the transport is git's, the crypto/auth hop is a local
// stand-in (see the package's TestGitSourceCloneWithDeployKey for the fake
// runner variant and the report for what a live host would still have to
// prove).
func TestGitSourceClonesPrivateRepoOverSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ssh stand-in is a POSIX shell script")
	}
	origin := seedGitFixture(t)
	origin = filepath.ToSlash(origin)

	stubDir := t.TempDir()
	recordPath := filepath.Join(stubDir, "record")
	keyCopyPath := filepath.Join(stubDir, "key.pem")
	if err := os.WriteFile(filepath.Join(stubDir, "ssh"), []byte(stubSSHScript), 0o755); err != nil {
		t.Fatalf("write ssh stand-in: %v", err)
	}
	t.Setenv("GOTHAM_TEST_SSH_RECORD", recordPath)
	t.Setenv("GOTHAM_TEST_SSH_KEY_COPY", keyCopyPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	// The URL shape the FE stores for a private provider repository: a
	// provider-reported ssh:// clone URL, here pointing at the fixture path.
	app := testApplication(uuid.New())
	app.CloneURL = "ssh://git@fixture.invalid" + origin
	app.Branch = "main"
	dir := filepath.Join(t.TempDir(), "repo")

	source := gitSource{keys: &staticKeyResolver{pem: privatePEM}}
	if err := source.Clone(context.Background(), app, dir, nil); err != nil {
		t.Fatalf("clone through the ssh stand-in: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		t.Errorf("cloned tree has no Dockerfile: %v", err)
	}

	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("the ssh stand-in never ran: %v", err)
	}
	text := string(record)
	for what, want := range map[string]string{
		"key flag":     "arg: -i\n",
		"key-only ssh": "IdentitiesOnly=yes",
		"known hosts":  "UserKnownHostsFile=",
		"remote git":   "git-upload-pack",
		"key mode":     "mode: 600",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ssh call is missing %s (%q):\n%s", what, want, text)
		}
	}
	copied, err := os.ReadFile(keyCopyPath)
	if err != nil {
		t.Fatalf("read the key the stand-in saw: %v", err)
	}
	if strings.TrimSpace(string(copied)) != strings.TrimSpace(privatePEM) {
		t.Error("the clone did not present the application's deploy key")
	}
}

// waitForPIDFile polls until the child wrote its pid.
func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid file %s was never written", path)
	return 0
}

// waitForProcessExit polls kill(pid, 0) until the process is gone.
func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive", pid)
}

// TestRunGitCancelKillsProcessGroup proves an aborted clone (step timeout,
// deploy cancellation, graceful service shutdown) takes down git *and* the
// children it spawned. The unit keeps KillMode=process, so systemd does not
// kill them; without the group kill the background sleep (standing in for
// ssh) would survive the cancellation.
func TestRunGitCancelKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are POSIX-only")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	script := "sleep 300 & echo $! > " + shellQuote(pidFile) + "; wait"
	done := make(chan error, 1)
	go func() {
		_, err := runGit(ctx, []string{"sh", "-c", script}, os.Environ())
		done <- err
	}()

	pid := waitForPIDFile(t, pidFile)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("runGit returned nil after the context was cancelled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runGit did not return after the context was cancelled")
	}
	waitForProcessExit(t, pid)
}

// TestRunGitChildDiesWithItsParent proves the Linux parent-death signal: a
// hard SIGKILL (or OOM) of the control plane leaves no chance to cancel
// contexts, so the kernel must kill the git child. The helper re-execs this
// test binary, starts a git command through runGit, reports the child's pid
// and exits without cleanup.
func TestRunGitChildDiesWithItsParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the parent-death signal is Linux-only")
	}
	helper := exec.Command(os.Args[0], "-test.run=TestRunGitChildDiesHelper")
	helper.Env = append(os.Environ(), "GOTHAM_RUN_GIT_DEATH_HELPER=1")
	output, err := helper.Output()
	if err != nil {
		t.Fatalf("helper failed: %v (output %q)", err, output)
	}
	value, ok := strings.CutPrefix(strings.TrimSpace(string(output)), "pid=")
	if !ok {
		t.Fatalf("helper output = %q, want pid=<n>", output)
	}
	pid, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("helper pid = %q: %v", value, err)
	}
	waitForProcessExit(t, pid)
}

// TestRunGitChildDiesHelper is the child of TestRunGitChildDiesWithItsParent.
func TestRunGitChildDiesHelper(t *testing.T) {
	if os.Getenv("GOTHAM_RUN_GIT_DEATH_HELPER") != "1" {
		t.Skip("helper process for TestRunGitChildDiesWithItsParent")
	}
	dir, err := os.MkdirTemp("", "gotham-git-death-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: %v\n", err)
		os.Exit(2)
	}
	pidFile := filepath.Join(dir, "child.pid")
	script := "echo $$ > " + shellQuote(pidFile) + "; exec sleep 300"
	go func() { _, _ = runGit(context.Background(), []string{"sh", "-c", script}, nil) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, readErr := os.ReadFile(pidFile); readErr == nil && strings.TrimSpace(string(data)) != "" {
			fmt.Printf("pid=%s\n", strings.TrimSpace(string(data)))
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "helper: the child never reported its pid")
	os.Exit(2)
}

// TestGitSourceKnownHostsPolicy pins the keyed clone's host-key policy: the
// embedded provider keys are always present, strict checking is the default,
// accept-new only appears under the dev flag, and GOTHAM_KNOWN_HOSTS is merged.
func TestGitSourceKnownHostsPolicy(t *testing.T) {
	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	app := testApplication(uuid.New())

	clone := func(t *testing.T, dir string) (command, knownHosts string) {
		t.Helper()
		run := func(_ context.Context, _ []string, env []string) ([]byte, error) {
			for _, entry := range env {
				if strings.HasPrefix(entry, "GIT_SSH_COMMAND=") {
					command = strings.TrimPrefix(entry, "GIT_SSH_COMMAND=")
				}
			}
			path := sshCommandKnownHostsPath(env)
			if path == "" {
				t.Error("GIT_SSH_COMMAND carries no UserKnownHostsFile path")
				return nil, nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Errorf("read known_hosts: %v", readErr)
				return nil, nil
			}
			knownHosts = string(data)
			return nil, nil
		}
		source := gitSource{keys: &staticKeyResolver{pem: privatePEM}, run: run}
		if err := source.Clone(context.Background(), app, dir, nil); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		return command, knownHosts
	}

	t.Run("strict by default", func(t *testing.T) {
		command, knownHosts := clone(t, filepath.Join(t.TempDir(), "repo"))
		if !strings.Contains(command, "StrictHostKeyChecking=yes") {
			t.Errorf("GIT_SSH_COMMAND = %q, want StrictHostKeyChecking=yes", command)
		}
		if strings.Contains(command, "accept-new") {
			t.Errorf("GIT_SSH_COMMAND = %q, want no accept-new by default", command)
		}
		// The host-wide known_hosts and KnownHostsCommand must not be trusted:
		// the ephemeral pinned set is the only anchor.
		if !strings.Contains(command, "GlobalKnownHostsFile=/dev/null") {
			t.Errorf("GIT_SSH_COMMAND = %q, want GlobalKnownHostsFile=/dev/null", command)
		}
		for _, host := range []string{
			"github.com ssh-ed25519", "gitlab.com ssh-ed25519",
			"bitbucket.org ssh-ed25519", "[ssh.github.com]:443 ssh-ed25519",
		} {
			if !strings.Contains(knownHosts, host) {
				t.Errorf("known_hosts is missing the pinned %q key:\n%s", host, knownHosts)
			}
		}
	})

	t.Run("dev flag warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		t.Setenv(devAcceptNewHostKeysEnv, "true")
		run := func(context.Context, []string, []string) ([]byte, error) { return nil, nil }
		source := gitSource{keys: &staticKeyResolver{pem: privatePEM}, run: run, logger: logger}
		if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		if !strings.Contains(buf.String(), devAcceptNewHostKeysEnv) {
			t.Errorf("warn log = %q, want it to name %s", buf.String(), devAcceptNewHostKeysEnv)
		}
	})

	t.Run("world-writable operator file warns", func(t *testing.T) {
		operatorFile := filepath.Join(t.TempDir(), "operator_known_hosts")
		if err := os.WriteFile(operatorFile, []byte("git.internal ssh-ed25519 AAAA\n"), 0o600); err != nil {
			t.Fatalf("write operator known_hosts: %v", err)
		}
		// WriteFile honours the umask; chmod makes the world-writable mode
		// deterministic across environments.
		if err := os.Chmod(operatorFile, 0o666); err != nil {
			t.Fatalf("chmod operator known_hosts: %v", err)
		}
		t.Setenv(knownHostsEnv, operatorFile)

		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		run := func(context.Context, []string, []string) ([]byte, error) { return nil, nil }
		source := gitSource{keys: &staticKeyResolver{pem: privatePEM}, run: run, logger: logger}
		if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		if !strings.Contains(buf.String(), "group/world-writable") {
			t.Errorf("warn log = %q, want a group/world-writable warning", buf.String())
		}
	})

	t.Run("accept-new only under the dev flag", func(t *testing.T) {
		t.Setenv(devAcceptNewHostKeysEnv, "true")
		command, _ := clone(t, filepath.Join(t.TempDir(), "repo"))
		if !strings.Contains(command, "StrictHostKeyChecking=accept-new") {
			t.Errorf("GIT_SSH_COMMAND = %q, want accept-new under %s", command, devAcceptNewHostKeysEnv)
		}
	})

	t.Run("operator known_hosts merged", func(t *testing.T) {
		operatorFile := filepath.Join(t.TempDir(), "operator_known_hosts")
		entry := "git.internal ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBtestoperatorkey\n"
		if err := os.WriteFile(operatorFile, []byte(entry), 0o600); err != nil {
			t.Fatalf("write operator known_hosts: %v", err)
		}
		t.Setenv(knownHostsEnv, operatorFile)

		command, knownHosts := clone(t, filepath.Join(t.TempDir(), "repo"))
		if !strings.Contains(command, "StrictHostKeyChecking=yes") {
			t.Errorf("GIT_SSH_COMMAND = %q, want strict checking with an operator file", command)
		}
		if !strings.Contains(knownHosts, "git.internal ssh-ed25519") {
			t.Errorf("known_hosts is missing the operator entry:\n%s", knownHosts)
		}
		if !strings.Contains(knownHosts, "github.com") {
			t.Errorf("known_hosts dropped the embedded provider keys:\n%s", knownHosts)
		}
	})
}

// stubSSHHost is a minimal in-process SSH server used to exercise OpenSSH host
// key verification with the exact options the cloner builds.
type stubSSHHost struct {
	listener net.Listener
	hostKey  ssh.PublicKey
}

// startStubSSHHost starts the server on a random loopback port.
func startStubSSHHost(t *testing.T) *stubSSHHost {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil // accept any public key
		},
	}
	config.AddHostKey(signer)

	host := &stubSSHHost{hostKey: signer.PublicKey()}
	host.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, acceptErr := host.listener.Accept()
			if acceptErr != nil {
				return
			}
			go host.handle(conn, config)
		}
	}()
	t.Cleanup(func() { _ = host.listener.Close() })
	return host
}

// handle performs the handshake and answers exec requests with success.
func (h *stubSSHHost) handle(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer func() { _ = channel.Close() }()
			for request := range requests {
				if request.Type != "exec" {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 0}))
				return
			}
		}()
	}
}

// knownHostsEntry renders a non-default-port known_hosts line for key.
func knownHostsEntry(host string, port int, key ssh.PublicKey) string {
	return "[" + host + "]:" + strconv.Itoa(port) + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

// writeOpenSSHKey writes a fresh ed25519 OpenSSH private key to path.
func writeOpenSSHKey(t *testing.T, path string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
}

// TestCloneRedactsURLCredentials is the item-3 regression: a clone URL that
// embeds a token must never reach the realtime deploy log — neither in the
// command line the cloner echoes nor in the quoted git error tail.
func TestCloneRedactsURLCredentials(t *testing.T) {
	const token = "ghp_supersecrettoken123"
	app := testApplication(uuid.New())
	app.CloneURL = "https://x-access-token:" + token + "@github.com/acme/demo.git"

	t.Run("log line", func(t *testing.T) {
		var lines []string
		run := func(context.Context, []string, []string) ([]byte, error) { return nil, nil }
		source := gitSource{run: run}
		if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"),
			func(line string) { lines = append(lines, line) }); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		joined := strings.Join(lines, "\n")
		if strings.Contains(joined, token) {
			t.Errorf("deploy log leaked the credential:\n%s", joined)
		}
		if !strings.Contains(joined, "github.com/acme/demo.git") {
			t.Errorf("deploy log = %q, want the host and path still logged", joined)
		}
	})

	t.Run("error tail", func(t *testing.T) {
		run := func(context.Context, []string, []string) ([]byte, error) {
			// A git version that echoes the URL it was handed, credential and all.
			return []byte("fatal: unable to access '" + app.CloneURL + "': auth failed"), errors.New("exit status 128")
		}
		source := gitSource{run: run}
		err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
		if err == nil {
			t.Fatal("Clone succeeded although git failed")
		}
		if strings.Contains(err.Error(), token) {
			t.Errorf("error leaked the credential: %v", err)
		}
	})
}

// TestRedactCloneURLAndError pins the redaction shapes directly, so the
// regex cannot silently widen (mangling harmless text) or narrow (leaving a
// bare-user credential in place).
func TestRedactCloneURLAndError(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://user:pass@host/repo.git", "https://host/repo.git"},
		{"https://token@host/repo.git", "https://host/repo.git"},
		{"git@host:acme/demo.git", "git@host:acme/demo.git"}, // scp-like, not a URL
		{"https://host/repo.git", "https://host/repo.git"},   // nothing to strip
		{"ssh://git@host/repo.git", "ssh://host/repo.git"},   // userinfo (git) hidden too
	}
	for _, tc := range cases {
		if got := redactCloneURL(tc.in); got != tc.want {
			t.Errorf("redactCloneURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := redactCloneError("fatal: could not read from '" + tc.in + "'"); strings.Contains(got, "pass@") || strings.Contains(got, "token@") {
			t.Errorf("redactCloneError left a credential in %q", got)
		}
	}
	// Harmless git diagnostics must survive untouched.
	plain := "fatal: repository 'https://github.com/acme/demo.git/' not found"
	if got := redactCloneError(plain); got != plain {
		t.Errorf("redactCloneError mangled a credential-free message: %q", got)
	}
}

// TestSSHHostKeyVerification runs the real OpenSSH client with the known_hosts
// and StrictHostKeyChecking options the cloner builds against an in-process
// host presenting an unknown key: strict refuses it, a pin accepts it, and
// accept-new learns it.
func TestSSHHostKeyVerification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ssh client stand-in is POSIX-only")
	}
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh binary is not available")
	}

	host := startStubSSHHost(t)
	_, portStr, err := net.SplitHostPort(host.listener.Addr().String())
	if err != nil {
		t.Fatalf("split host address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	clientKey := filepath.Join(t.TempDir(), "id_ed25519")
	writeOpenSSHKey(t, clientKey)

	run := func(knownHostsPath string, acceptNew bool) (string, error) {
		// Mirror the options the cloner's GIT_SSH_COMMAND sets, plus BatchMode
		// so a prompt can never block the test.
		args := []string{
			"-i", clientKey,
			"-o", "IdentitiesOnly=yes",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=5",
			"-o", "UserKnownHostsFile=" + knownHostsPath,
			"-o", "StrictHostKeyChecking=" + strictHostKeyChecking(acceptNew),
			"-p", portStr,
			"tester@127.0.0.1",
			"true",
		}
		out, runErr := exec.Command(sshBin, args...).CombinedOutput()
		return string(out), runErr
	}

	newKnownHosts := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write empty known_hosts: %v", err)
		}
		return path
	}

	t.Run("unknown key refused", func(t *testing.T) {
		out, err := run(newKnownHosts(t), false)
		if err == nil {
			t.Fatalf("ssh accepted an unknown host key; output:\n%s", out)
		}
		if !strings.Contains(out, "Host key verification failed") {
			t.Errorf("output = %q, want a host key verification failure", out)
		}
	})

	t.Run("pinned key accepted", func(t *testing.T) {
		path := newKnownHosts(t)
		line := knownHostsEntry("127.0.0.1", port, host.hostKey) + "\n"
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatalf("write pinned known_hosts: %v", err)
		}
		if out, err := run(path, false); err != nil {
			t.Fatalf("ssh refused the pinned host key: %v\n%s", err, out)
		}
	})

	t.Run("accept-new learns the key", func(t *testing.T) {
		path := newKnownHosts(t)
		if out, err := run(path, true); err != nil {
			t.Fatalf("accept-new refused an unknown host key: %v\n%s", err, out)
		}
		// Ubuntu's ssh_config enables HashKnownHosts, so the learned entry is
		// hashed (|1|…) and a plain-text contains check fails there. ssh-keygen
		// -F resolves hashed and plain entries alike.
		keygen, err := exec.LookPath("ssh-keygen")
		if err != nil {
			t.Skip("ssh-keygen binary is not available")
		}
		out, err := exec.Command(keygen, "-F", "[127.0.0.1]:"+portStr, "-f", path).CombinedOutput()
		if err != nil {
			data, _ := os.ReadFile(path)
			t.Errorf("accept-new did not learn the host key (%v, output %q):\n%s", err, out, data)
		}
	})
}
