package deploy

import (
	"context"
	"errors"
	"fmt"
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

	if got := countEntry(gotEnv, "GIT_SSH_COMMAND"); got != 1 {
		t.Fatalf("GIT_SSH_COMMAND entries = %d in env %v, want exactly 1", got, gotEnv)
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
	if keyPath != "" && !strings.Contains(command, "-i '"+keyPath+"'") {
		t.Errorf("GIT_SSH_COMMAND = %q, want it to carry -i %q", command, keyPath)
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
