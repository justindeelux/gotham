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

// TestValidatePublicGitURL pins the GS-3 allow-list: a keyless public source
// takes http, https or git and nothing else. SSH transports are refused by
// name (the refusal is what keeps a keyless clone off the control plane's
// ambient SSH identity), and local sources stay behind the dev flag.
func TestValidatePublicGitURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		want    bool
		wantMsg string
	}{
		{"https", "https://github.com/acme/demo.git", true, ""},
		{"http", "http://git.internal/acme/demo.git", true, ""},
		{"git scheme", "git://git.internal/acme/demo.git", true, ""},
		{"https with port", "https://gitea.example:3000/acme/demo.git", true, ""},
		{"uppercase scheme", "HTTPS://github.com/acme/demo.git", true, ""},
		{"ssh scheme", "ssh://git@git.internal/acme/demo.git", false, "SSH"},
		{"ssh with port", "ssh://git@git.internal:2222/acme/demo.git", false, "SSH"},
		{"scp-like", "git@git.internal:acme/demo.git", false, "SSH"},
		{"scp-like other user", "deploy@git.internal:acme/demo.git", false, "SSH"},
		{"ftp scheme", "ftp://example.com/demo.git", false, "unsupported clone URL scheme"},
		{"empty", "", false, "no clone URL"},
		{"bare name", "demo", false, "unsupported clone URL"},
		{"option injection", "--upload-pack=touch /tmp/pwn", false, "unsupported clone URL"},
		{"local path", "/srv/fixtures/demo", false, "local clone sources are disabled"},
		{"file URL", "file:///srv/fixtures/demo", false, "local clone sources are disabled"},
		{"embedded token", "https://user:ghp_SECRET@github.com/acme/demo.git", false, "must not embed credentials"},
		{"bare username", "https://token@github.com/acme/demo.git", false, "must not embed credentials"},
		{"empty host", "https://", false, "no host"},
		{"hostless path", "https:///acme/demo.git", false, "no host"},
		{"space in path", "https://host/a b.git", false, "whitespace"},
		{"newline smuggle", "https://h/x\n--upload-pack=touch", false, "whitespace"},
		{"git+ssh scheme", "git+ssh://host/acme/demo.git", false, "unsupported clone URL scheme"},
		{"ext scheme", "ext::sh -c id", false, "unsupported clone URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePublicGitURL(tc.url)
			if tc.want {
				if err != nil {
					t.Fatalf("ValidatePublicGitURL(%q) = %v, want nil", tc.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePublicGitURL(%q) = nil, want an error", tc.url)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want it wrapped in ErrValidation", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantMsg)
			}
		})
	}
}

// TestValidatePublicGitURLDevLocal covers the fixture hatch: local paths and
// file:// URLs validate only in dev mode.
func TestValidatePublicGitURLDevLocal(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	for _, url := range []string{"/srv/fixtures/demo", "file:///srv/fixtures/demo"} {
		if err := ValidatePublicGitURL(url); err != nil {
			t.Errorf("ValidatePublicGitURL(%q) with %s=true = %v, want nil", url, devLocalCloneEnv, err)
		}
	}
}

// TestIsSSHTransportURL pins the deploy-time SSH detector: ssh:// and both
// scp spellings refuse a keyless clone, while public schemes and local
// fixtures pass through.
func TestIsSSHTransportURL(t *testing.T) {
	for url, want := range map[string]bool{
		"ssh://git@host/acme/demo.git": true,
		"SSH://git@host/acme/demo.git": true,
		"git@host:acme/demo.git":       true,
		"deploy@host:acme/demo.git":    true,
		"https://host/acme/demo.git":   false,
		"http://host/acme/demo.git":    false,
		"git://host/acme/demo.git":     false,
		"/srv/fixtures/demo":           false,
		"file:///srv/fixtures/demo":    false,
		"--upload-pack=touch /tmp/pwn": false,
	} {
		if got := isSSHTransportURL(url); got != want {
			t.Errorf("isSSHTransportURL(%q) = %v, want %v", url, got, want)
		}
	}
}

// TestParseSymrefBranch pins the ls-remote --symref parsing: the HEAD symref
// resolves, detached/missing output answers "".
func TestParseSymrefBranch(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{"main", "ref: refs/heads/main\tHEAD\nabc123\tHEAD\n", "main"},
		{"other default", "ref: refs/heads/trunk\tHEAD\nabc123\tHEAD\n", "trunk"},
		{"detached head", "abc123\tHEAD\n", ""},
		{"empty", "", ""},
		{"unsafe name", "ref: refs/heads/-evil\tHEAD\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSymrefBranch(tc.output); got != tc.want {
				t.Errorf("parseSymrefBranch(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

// TestDefaultBranchForLsRemote pins the ls-remote invocation (argv, HEAD)
// and the symref parsing through a fake runner: no network involved.
func TestDefaultBranchForLsRemote(t *testing.T) {
	const url = "https://git.example/acme/demo.git"
	var gotArgv []string
	run := func(context.Context, []string, []string) ([]byte, error) {
		return []byte("ref: refs/heads/trunk\tHEAD\n0123456789abcdef\tHEAD\n"), nil
	}
	branch, err := defaultBranchFor(context.Background(), func(ctx context.Context, argv, env []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		return run(ctx, argv, env)
	}, url, nil)
	if err != nil {
		t.Fatalf("defaultBranchFor: %v", err)
	}
	if branch != "trunk" {
		t.Errorf("branch = %q, want %q (must not guess main)", branch, "trunk")
	}
	joined := strings.Join(gotArgv, " ")
	for _, want := range []string{"ls-remote", "--symref", "HEAD", url} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv = %v, want it to contain %q", gotArgv, want)
		}
	}
}

// TestDefaultBranchForFailure pins the actionable error: an unreachable host
// fails the resolution with the quoted detail followed by the hint.
func TestDefaultBranchForFailure(t *testing.T) {
	run := func(context.Context, []string, []string) ([]byte, error) {
		return []byte("fatal: Could not resolve host: git.example"), errors.New("exit status 128")
	}
	_, err := defaultBranchFor(context.Background(), run, "https://git.example/acme/demo.git", nil)
	if err == nil {
		t.Fatal("err = nil, want the resolution to fail")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("err = %v, want the unreachable hint", err)
	}
	// The hint trails the quoted git detail instead of leading it.
	detailAt := strings.Index(err.Error(), "Could not resolve host")
	hintAt := strings.Index(err.Error(), "unreachable")
	if detailAt == -1 || hintAt == -1 || hintAt < detailAt {
		t.Errorf("err = %v, want the detail quoted before the hint", err)
	}
}

// TestClassifyGitFailure pins the hint classes against realistic git stderr:
// a missing branch names the branch (not the repository), a tty-less 401
// asks for credentials, and a port number never triggers the auth hint.
func TestClassifyGitFailure(t *testing.T) {
	cases := []struct {
		output string
		hint   string
	}{
		{"fatal: Remote branch foo not found in upstream origin", "branch not found"},
		{"fatal: repository 'https://host/acme/demo.git/' not found", "repository not found"},
		{"fatal: could not read Username for 'https://host/acme/demo.git': terminal prompts disabled", "authentication"},
		{"fatal: Authentication failed for 'https://host/acme/demo.git/'", "authentication"},
		{"fatal: could not read Username for 'http://127.0.0.1:18401/x.git': No such device or address", "authentication"},
		{"fatal: unable to access 'https://host/x.git/': The requested URL returned error: 403", "authentication"},
		{"fatal: unable to access 'https://host/x.git/': Could not resolve host: host", "unreachable"},
		{"fatal: unable to access 'http://127.0.0.1:18401/x.git/': Failed to connect: Connection refused", "unreachable"},
		{"ssh: connect to host host port 22: Connection refused", "unreachable"},
		{"fatal: Could not read from remote repository.", "authentication"},
		{"some new git message", ""},
	}
	for _, tc := range cases {
		got := classifyGitFailure(tc.output)
		if tc.hint == "" {
			if got != "" {
				t.Errorf("classifyGitFailure(%q) = %q, want no hint", tc.output, got)
			}
			continue
		}
		if !strings.Contains(got, tc.hint) {
			t.Errorf("classifyGitFailure(%q) = %q, want it to mention %q", tc.output, got, tc.hint)
		}
	}
}

// TestCloneRefusesKeylessSSH pins the ambient-identity guard: an SSH URL with
// no deploy key is refused before git runs, for both URL spellings.
func TestCloneRefusesKeylessSSH(t *testing.T) {
	for _, url := range []string{
		"ssh://git@git.internal/acme/demo.git",
		"git@git.internal:acme/demo.git",
	} {
		t.Run(url, func(t *testing.T) {
			app := testApplication(uuid.New())
			app.SourceType = SourceGitPublic
			app.Provider = ""
			app.CloneURL = url
			app.Branch = "main"
			ran := false
			source := gitSource{
				keys: &staticKeyResolver{},
				run: func(context.Context, []string, []string) ([]byte, error) {
					ran = true
					return nil, nil
				},
			}
			err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
			if err == nil {
				t.Fatal("err = nil, want the keyless SSH clone refused")
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want it wrapped in ErrValidation", err)
			}
			if !strings.Contains(err.Error(), "deploy key") {
				t.Errorf("err = %v, want it to name the deploy key", err)
			}
			if ran {
				t.Error("git ran for a refused keyless SSH clone")
			}
		})
	}
}

// TestCloneFailureHints pins the actionable deploy-log errors: a failing
// clone quotes git and appends the classified hint.
func TestCloneFailureHints(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		hint   string
	}{
		{"not found", "fatal: repository 'https://host/acme/demo.git/' not found", "not found"},
		{"needs auth", "fatal: Authentication failed for 'https://host/acme/demo.git/'", "authentication"},
		{"unreachable", "fatal: unable to access 'https://host/x.git/': Could not resolve host: host", "unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := testApplication(uuid.New())
			app.SourceType = SourceGitPublic
			app.Provider = ""
			app.Branch = "main"
			source := gitSource{
				run: func(context.Context, []string, []string) ([]byte, error) {
					return []byte(tc.stderr), errors.New("exit status 128")
				},
			}
			err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
			if err == nil {
				t.Fatal("err = nil, want the failed clone surfaced")
			}
			if !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("err = %v, want the %q hint", err, tc.hint)
			}
		})
	}
}

// TestCloneResolvesEmptyBranch pins the GS-3 default: no branch means
// ls-remote decides, against a real local fixture (no network).
func TestCloneResolvesEmptyBranch(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	origin := seedGitFixture(t)

	app := testApplication(uuid.New())
	app.SourceType = SourceGitPublic
	app.Provider = ""
	app.CloneURL = origin
	app.Branch = ""
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
		t.Errorf("log = %q, want the resolved default branch", joined)
	}
}

// TestCloneResolvesNonMainDefault proves the resolution is not a hardcoded
// "main": the fixture default is renamed and the clone follows it.
func TestCloneResolvesNonMainDefault(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	origin := seedGitFixture(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary is not available")
	}
	cmd := exec.Command(git, "-C", origin, "branch", "-M", "trunk")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rename fixture branch: %v\n%s", err, out)
	}

	app := testApplication(uuid.New())
	app.SourceType = SourceGitPublic
	app.Provider = ""
	app.CloneURL = origin
	app.Branch = ""
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
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "--branch trunk") {
		t.Errorf("log = %q, want the renamed default branch", joined)
	}
}

// TestCreateApplicationKeepsEmptyBranchForPublicGit pins the GS-3 fix-round-1
// rule end to end at the service boundary: a git_public application created
// with no branch stores "", so the deploy clone resolves the remote default
// via ls-remote. Provider flows keep the "main" fallback.
func TestCreateApplicationKeepsEmptyBranchForPublicGit(t *testing.T) {
	t.Run("public git keeps the empty branch", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		in := validCreateInput(uuid.New())
		in.SourceType = SourceGitPublic
		in.Provider = ""
		in.Repo = "https://github.com/acme/demo.git"
		in.CloneURL = "https://github.com/acme/demo.git"
		in.Branch = ""
		created, err := svc.CreateApplication(context.Background(), uuid.New(), in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.Branch != "" {
			t.Errorf("branch = %q, want it kept empty for ls-remote resolution", created.Branch)
		}
	})

	t.Run("provider flow still defaults to main", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		in := validCreateInput(uuid.New())
		in.SourceType = SourceGitHubApp
		in.Provider = "github"
		in.Branch = ""
		created, err := svc.CreateApplication(context.Background(), uuid.New(), in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.Branch != defaultBranch {
			t.Errorf("branch = %q, want the %q fallback", created.Branch, defaultBranch)
		}
	})
}

// TestUpdateApplicationKeepsEmptyBranchForPublicGit pins the update half:
// clearing the branch restores ls-remote resolution for public-git sources.
func TestUpdateApplicationKeepsEmptyBranchForPublicGit(t *testing.T) {
	empty := ""
	newPublicApp := func() Application {
		app := testApplication(uuid.New())
		app.SourceType = SourceGitPublic
		app.Provider = ""
		app.CloneURL = "https://github.com/acme/demo.git"
		app.Branch = "main"
		return app
	}

	t.Run("public git clears to empty", func(t *testing.T) {
		app := newPublicApp()
		repo := &fakeRepository{app: app}
		svc := newTestService(t, repo)
		updated, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID,
			UpdateApplicationInput{Branch: &empty})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.Branch != "" {
			t.Errorf("branch = %q, want it cleared for ls-remote resolution", updated.Branch)
		}
	})

	t.Run("provider flow falls back to main", func(t *testing.T) {
		app := testApplication(uuid.New()) // legacy empty type, github provider
		repo := &fakeRepository{app: app}
		svc := newTestService(t, repo)
		updated, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID,
			UpdateApplicationInput{Branch: &empty})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.Branch != defaultBranch {
			t.Errorf("branch = %q, want the %q fallback", updated.Branch, defaultBranch)
		}
	})
}

// TestCloneSourceResolvesMasterDefault is the finding-1 regression at the
// deploy fetch step: a stored git_public application with an empty branch
// clones the fixture's non-main default (master), not a guessed "main".
func TestCloneSourceResolvesMasterDefault(t *testing.T) {
	t.Setenv(devLocalCloneEnv, "true")
	origin := seedGitFixture(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary is not available")
	}
	cmd := exec.Command(git, "-C", origin, "branch", "-M", "master")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rename fixture branch: %v\n%s", err, out)
	}

	app := testApplication(uuid.New())
	app.SourceType = SourceGitPublic
	app.Provider = ""
	app.CloneURL = origin
	app.Branch = ""
	dir := filepath.Join(t.TempDir(), "repo")

	var lines []string
	o := newTestOrchestrator(Config{Source: gitSource{}})
	if err := o.cloneSource(context.Background(), app, dir, func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("cloneSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		t.Errorf("cloned tree has no Dockerfile: %v", err)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "--branch master") {
		t.Errorf("log = %q, want the master default", joined)
	}
}

// TestApplicationResponseRedactsCredentials pins the API echo guard: a
// legacy token-bearing clone URL is returned without its userinfo.
func TestApplicationResponseRedactsCredentials(t *testing.T) {
	app := testApplication(uuid.New())
	app.CloneURL = "https://user:ghp_SECRET@github.com/acme/demo.git"
	got := newApplicationResponse(app)
	if strings.Contains(got.CloneURL, "ghp_SECRET") || strings.Contains(got.CloneURL, "user@") {
		t.Errorf("clone_url = %q, want the credentials stripped", got.CloneURL)
	}
	if got.CloneURL != "https://github.com/acme/demo.git" {
		t.Errorf("clone_url = %q, want the clean public URL", got.CloneURL)
	}
}
