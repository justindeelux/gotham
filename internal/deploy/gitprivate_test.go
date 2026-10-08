package deploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// privateTestApp returns a provider-less git_private application: the shape
// the GS-4 wizard submits (empty provider, the clone URL as the repo label).
func privateTestApp(userID uuid.UUID, cloneURL string) Application {
	app := testApplication(userID)
	app.SourceType = SourceGitPrivate
	app.Provider = ""
	app.Repo = cloneURL
	app.CloneURL = cloneURL
	return app
}

// staticCredResolver hands out a fixed HTTPS credential for the cloner.
type staticCredResolver struct {
	username string
	token    string
	err      error
	calls    int
}

// Compile-time guarantee that staticCredResolver satisfies the seam.
var _ gitCredentialResolver = (*staticCredResolver)(nil)

// GitCredential implements gitCredentialResolver.
func (r *staticCredResolver) GitCredential(_ context.Context, _ uuid.UUID) (string, string, error) {
	r.calls++
	if r.err != nil {
		return "", "", r.err
	}
	return r.username, r.token, nil
}

func TestValidatePrivateGitURL(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		want  bool
		check error
	}{
		{"ssh URL", "ssh://git@git.internal/acme/demo.git", true, nil},
		{"ssh URL with port", "ssh://git@git.internal:2222/acme/demo.git", true, nil},
		{"scp-like", "git@git.internal:acme/demo.git", true, nil},
		{"scp-like with user", "deploy@git.internal:acme/demo.git", true, nil},
		{"https URL", "https://git.internal/acme/demo.git", true, nil},
		{"http URL", "http://git.internal/acme/demo.git", true, nil},
		{"https userinfo refused", "https://user:token@git.internal/acme/demo.git", false, ErrValidation},
		{"ssh userinfo refused", "ssh://user:pass@git.internal/acme/demo.git", false, ErrValidation},
		{"git scheme refused", "git://git.internal/acme/demo.git", false, ErrValidation},
		{"ftp refused", "ftp://example.com/demo.git", false, ErrValidation},
		{"empty", "", false, ErrValidation},
		{"whitespace smuggling", "https://git.internal/acme/demo.git --upload-pack=touch", false, ErrValidation},
		{"bare name", "demo", false, ErrValidation},
		{"local path denied by default", "/srv/fixtures/demo", false, ErrValidation},
		{"file URL denied by default", "file:///srv/fixtures/demo", false, ErrValidation},
		{"ssh URL without host", "ssh://", false, ErrValidation},
		{"https URL without host", "https:///acme/demo.git", false, ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePrivateGitURL(tc.url)
			if tc.want {
				if err != nil {
					t.Fatalf("ValidatePrivateGitURL(%q) = %v, want nil", tc.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePrivateGitURL(%q) = nil, want an error", tc.url)
			}
			if tc.check != nil && !errors.Is(err, tc.check) {
				t.Errorf("err = %v, want it wrapped in %v", err, tc.check)
			}
		})
	}

	t.Run("dev local sources", func(t *testing.T) {
		t.Setenv(devLocalCloneEnv, "true")
		for _, url := range []string{"/srv/fixtures/demo", "file:///srv/fixtures/demo"} {
			if err := ValidatePrivateGitURL(url); err != nil {
				t.Errorf("ValidatePrivateGitURL(%q) with %s=true = %v, want nil", url, devLocalCloneEnv, err)
			}
		}
	})
}

func TestPrivateGitHost(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"ssh://git@git.internal:2222/acme/demo.git", "git.internal"},
		{"git@git.internal:acme/demo.git", "git.internal"},
		{"deploy@git.internal:acme/demo.git", "git.internal"},
		{"https://git.internal/acme/demo.git", "git.internal"},
		{"https://user:token@git.internal/acme/demo.git", "git.internal"},
		{"not a url", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := privateGitHost(tc.url); got != tc.want {
			t.Errorf("privateGitHost(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestBranchDefaultsToRemote(t *testing.T) {
	cases := []struct {
		sourceType string
		provider   string
		want       bool
	}{
		{SourceGitPublic, "", true},
		{"", "", true},
		{SourceGitPrivate, "", true},
		{SourceGitHubApp, "github", false},
		{SourceGitLabApp, "gitlab", false},
		{SourceDockerfile, "", false},
	}
	for _, tc := range cases {
		if got := BranchDefaultsToRemote(tc.sourceType, tc.provider); got != tc.want {
			t.Errorf("BranchDefaultsToRemote(%q, %q) = %v, want %v",
				tc.sourceType, tc.provider, got, tc.want)
		}
	}
}

// TestCreateDeployKeyLocalPath covers the GS-4 provider-less key: with no
// registrar wired (there is no Git-host API for an arbitrary remote) the key
// is generated and stored locally, a second call returns the same row, and
// the private half opens for the cloner while the service read carries the
// public half only.
func TestCreateDeployKeyLocalPath(t *testing.T) {
	userID := uuid.New()
	app := privateTestApp(userID, "git@git.internal:acme/demo.git")
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	key, err := svc.CreateDeployKey(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}
	if key.Provider != "" || key.ProviderKeyID != "" {
		t.Errorf("key = %+v, want a local key with no provider or provider key id", key)
	}
	if key.PublicKey == "" || !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") {
		t.Errorf("public key = %q, want an OpenSSH ed25519 line", key.PublicKey)
	}
	if key.Fingerprint == "" {
		t.Error("fingerprint is empty")
	}

	again, err := svc.CreateDeployKey(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("second CreateDeployKey: %v", err)
	}
	if again.ID != key.ID || again.PublicKey != key.PublicKey {
		t.Error("the second create did not return the stored key (it must be idempotent)")
	}

	opened, err := repo.DeployKeyPrivatePEM(context.Background(), app.ID)
	if err != nil || !strings.Contains(opened, "OPENSSH PRIVATE KEY") {
		t.Errorf("DeployKeyPrivatePEM = %q, %v; want the stored private half", opened, err)
	}

	read, err := svc.GetDeployKey(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("GetDeployKey: %v", err)
	}
	if read.PublicKey != key.PublicKey || read.Fingerprint != key.Fingerprint {
		t.Error("GetDeployKey did not return the stored public half")
	}
}

func TestCreateDeployKeyLocalRequiresRepository(t *testing.T) {
	userID := uuid.New()
	app := privateTestApp(userID, "")
	app.Repo = ""
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	if _, err := svc.CreateDeployKey(context.Background(), userID, app.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestCreateDeployKeyProviderPathStillNeedsRegistrar pins the untouched
// provider flow: a connected-provider application still registers through
// the Git-host API, so a missing registrar is a clear error, not a silent
// local key.
func TestCreateDeployKeyProviderPathStillNeedsRegistrar(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceGitHubApp
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	if _, err := svc.CreateDeployKey(context.Background(), userID, app.ID); err == nil ||
		!strings.Contains(err.Error(), "registrar") {
		t.Fatalf("err = %v, want the missing-registrar error", err)
	}
}

// TestSetGitCredential seals the token and never returns it: the stored row
// differs from the plaintext and opens back to it, rotation replaces the row,
// and bad inputs fail before anything is stored.
func TestSetGitCredential(t *testing.T) {
	userID := uuid.New()
	app := privateTestApp(userID, "https://git.internal/acme/demo.git")
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	state, err := svc.SetGitCredential(context.Background(), userID, app.ID, "bob", "s3cr3t-token")
	if err != nil {
		t.Fatalf("SetGitCredential: %v", err)
	}
	if !state.HasCredential || state.Username != "bob" {
		t.Errorf("state = %+v, want the username and a set credential", state)
	}
	stored := repo.gitCreds[app.ID]
	if stored.sealed == "s3cr3t-token" || stored.sealed == "" {
		t.Error("the token was stored in the clear (or not at all)")
	}
	if opened, err := providers.OpenSecret(testSecretKey, stored.sealed); err != nil || opened != "s3cr3t-token" {
		t.Errorf("stored credential opens to %q, %v; want the token", opened, err)
	}
	if stored.username != "bob" {
		t.Errorf("username = %q, want bob", stored.username)
	}

	if _, err := svc.SetGitCredential(context.Background(), userID, app.ID, "bob", "rotated"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, token, err := repo.GitCredential(context.Background(), app.ID); err != nil || token != "rotated" {
		t.Errorf("rotated credential opens to %q, %v; want the new token", token, err)
	}

	for name, pair := range map[string][2]string{
		"empty token":     {"bob", ""},
		"blank token":     {"bob", "   "},
		"oversized token": {"bob", strings.Repeat("x", maxGitTokenLength+1)},
		"oversized user":  {strings.Repeat("u", 256), "token"},
	} {
		if _, err := svc.SetGitCredential(context.Background(), userID, app.ID, pair[0], pair[1]); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}

	t.Run("non-private source is refused", func(t *testing.T) {
		other := testApplication(userID)
		other.SourceType = SourceGitPublic
		otherRepo := &fakeRepository{app: other}
		otherSvc := newTestService(t, otherRepo)
		if _, err := otherSvc.SetGitCredential(context.Background(), userID, other.ID, "", "token"); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		if len(otherRepo.gitCreds) != 0 {
			t.Error("a refused credential was stored")
		}
	})

	t.Run("disabled surface", func(t *testing.T) {
		t.Setenv(FeatureEnv, "false")
		if _, err := svc.SetGitCredential(context.Background(), userID, app.ID, "", "token"); !errors.Is(err, ErrDisabled) {
			t.Fatalf("err = %v, want ErrDisabled", err)
		}
	})
}

func TestGetGitCredential(t *testing.T) {
	userID := uuid.New()
	app := privateTestApp(userID, "https://git.internal/acme/demo.git")
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	empty, err := svc.GetGitCredential(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("GetGitCredential: %v", err)
	}
	if empty.HasCredential || empty.Username != "" {
		t.Errorf("state = %+v, want no credential", empty)
	}

	if _, err := svc.SetGitCredential(context.Background(), userID, app.ID, "bob", "s3cr3t"); err != nil {
		t.Fatalf("SetGitCredential: %v", err)
	}
	state, err := svc.GetGitCredential(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("GetGitCredential: %v", err)
	}
	if !state.HasCredential || state.Username != "bob" {
		t.Errorf("state = %+v, want the username and a set credential", state)
	}
}

// TestCloneCredentialAskpass pins the HTTPS injection: the token reaches git
// only inside the ephemeral helper file (never argv, env values or logs),
// the helper is 0700, the environment is deterministic, the URL passes after
// "--", and the helper is gone when the clone returns.
func TestCloneCredentialAskpass(t *testing.T) {
	const token = "s3cr3t-token-value"
	app := privateTestApp(uuid.New(), "https://git.internal/acme/demo.git")
	app.Branch = "main"
	dir := filepath.Join(t.TempDir(), "repo")
	src := gitSource{creds: &staticCredResolver{username: "bob", token: token}}

	var gotArgv, gotEnv []string
	var helperPath string
	run := func(_ context.Context, argv, env []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		gotEnv = append([]string(nil), env...)
		for _, entry := range env {
			if value, ok := strings.CutPrefix(entry, "GIT_ASKPASS="); ok {
				helperPath = value
				info, err := os.Stat(value)
				if err != nil {
					t.Errorf("stat askpass helper: %v", err)
				} else if perm := info.Mode().Perm(); perm != 0o700 {
					t.Errorf("askpass helper mode = %o, want 700", perm)
				}
			}
		}
		return nil, nil
	}
	src.run = run

	var lines []string
	if err := src.Clone(context.Background(), app, dir, func(line string) { lines = append(lines, line) }); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// The URL passes after "--" so a hostile value is never parsed as a flag.
	dash := -1
	for i, arg := range gotArgv {
		if arg == "--" {
			dash = i
		}
	}
	if dash < 0 || dash+1 >= len(gotArgv) || gotArgv[dash+1] != app.CloneURL {
		t.Errorf("argv = %v, want the clone URL right after --", gotArgv)
	}
	joined := strings.Join(gotArgv, " ") + "\n" + strings.Join(gotEnv, "\n") + "\n" + strings.Join(lines, "\n")
	if strings.Contains(joined, token) {
		t.Error("the token reached argv, the environment or the log")
	}
	if hasEntry(gotEnv, "GIT_SSH_COMMAND") {
		t.Errorf("env = %v, want no GIT_SSH_COMMAND on the HTTPS path", gotEnv)
	}
	if got := countEntry(gotEnv, "GIT_ASKPASS"); got != 1 {
		t.Fatalf("GIT_ASKPASS entries = %d in env %v, want exactly 1", got, gotEnv)
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_KEY_0=credential.helper"} {
		if !hasEntry(gotEnv, strings.SplitN(want, "=", 2)[0]) {
			t.Errorf("env = %v, want %s", gotEnv, want)
		}
	}
	if helperPath == "" {
		t.Fatal("GIT_ASKPASS carries no helper path")
	}
	if _, err := os.Stat(helperPath); !os.IsNotExist(err) {
		t.Errorf("askpass helper survived the clone (stat = %v), want it removed", err)
	}
	joinedLog := strings.Join(lines, "\n")
	if !strings.Contains(joinedLog, "(using the stored HTTPS credential)") {
		t.Errorf("log = %q, want the credential marker", joinedLog)
	}
	if !strings.Contains(joinedLog, app.CloneURL) {
		t.Errorf("log = %q, want the clone URL", joinedLog)
	}
}

// TestCloneCredentialDeployKeyFallback pins the compatibility path: an https
// URL with a deploy key but no token is rewritten to SSH and cloned keyed,
// exactly like before GS-4.
func TestCloneCredentialDeployKeyFallback(t *testing.T) {
	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	app := privateTestApp(uuid.New(), "https://git.internal/acme/demo.git")
	app.Branch = "main"
	src := gitSource{keys: &staticKeyResolver{pem: privatePEM}}

	var gotArgv, gotEnv []string
	src.run = func(_ context.Context, argv, env []string) ([]byte, error) {
		gotArgv, gotEnv = argv, env
		return nil, nil
	}
	if err := src.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if joined := strings.Join(gotArgv, " "); !strings.Contains(joined, "ssh://git@git.internal/acme/demo.git") {
		t.Errorf("argv = %v, want the URL rewritten to SSH", gotArgv)
	}
	if !hasEntry(gotEnv, "GIT_SSH_COMMAND") {
		t.Errorf("env = %v, want GIT_SSH_COMMAND for the keyed fallback", gotEnv)
	}
}

// TestCloneCredentialSSHRefusals pins the SSH guards: without a deploy key
// the clone is refused (never ambient-identity), and a stored HTTPS token
// does not satisfy an SSH URL.
func TestCloneCredentialSSHRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		url   string
		keys  deployKeyResolver
		creds gitCredentialResolver
	}{
		{"no credential at all", "git@git.internal:acme/demo.git", &staticKeyResolver{}, nil},
		{"token does not satisfy SSH", "git@git.internal:acme/demo.git", &staticKeyResolver{}, &staticCredResolver{token: "s3cr3t"}},
		{"ssh scheme without key", "ssh://git@git.internal/acme/demo.git", &staticKeyResolver{}, &staticCredResolver{token: "s3cr3t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := privateTestApp(uuid.New(), tc.url)
			ran := false
			src := gitSource{keys: tc.keys, creds: tc.creds}
			src.run = func(context.Context, []string, []string) ([]byte, error) {
				ran = true
				return nil, nil
			}
			err := src.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if ran {
				t.Error("git ran for a refused URL")
			}
		})
	}
}

// TestAskpassHelperExecution runs the generated helper with a real shell:
// the username prompt answers the username, every other prompt the token.
// It also pins the 0700 mode and the cleanup.
func TestAskpassHelperExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the helper is a POSIX shell script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	files, err := newAskpassFiles("bob", "s3cr3tTOK'en\"x")
	if err != nil {
		t.Fatalf("newAskpassFiles: %v", err)
	}
	info, err := os.Stat(files.scriptPath)
	if err != nil {
		t.Fatalf("stat helper: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("helper mode = %o, want 700", perm)
	}
	run := func(prompt string) string {
		cmd := exec.Command(sh, files.scriptPath, prompt)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("helper(%q): %v", prompt, err)
		}
		return string(out)
	}
	if got := run("Username for 'https://git.internal': "); got != "bob" {
		t.Errorf("username prompt answered %q, want bob", got)
	}
	if got := run("Password for 'https://git.internal': "); got != "s3cr3tTOK'en\"x" {
		t.Errorf("password prompt answered %q, want the token verbatim", got)
	}
	dir := files.dir
	files.remove()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("helper directory survived (stat = %v), want it removed", err)
	}
}

// TestGitSourceClonesPrivateGitScpURL exercises the scp-like private URL end
// to end without a network: a real git binary clones a real fixture through
// the GIT_SSH_COMMAND the cloner builds, with the stub ssh on PATH (the same
// stand-in TestGitSourceClonesPrivateRepoOverSSH uses).
func TestGitSourceClonesPrivateGitScpURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ssh stand-in is a POSIX shell script")
	}
	origin := seedGitFixture(t)

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
	app := privateTestApp(uuid.New(), "git@fixture.invalid:"+filepath.ToSlash(origin))
	app.Branch = "main"
	dir := filepath.Join(t.TempDir(), "repo")

	if err := (gitSource{keys: &staticKeyResolver{pem: privatePEM}}).Clone(context.Background(), app, dir, nil); err != nil {
		t.Fatalf("clone through the ssh stand-in: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		t.Errorf("cloned tree has no Dockerfile: %v", err)
	}
	copied, err := os.ReadFile(keyCopyPath)
	if err != nil {
		t.Fatalf("read the key the stand-in saw: %v", err)
	}
	if strings.TrimSpace(string(copied)) != strings.TrimSpace(privatePEM) {
		t.Error("the clone did not present the application's deploy key")
	}
}

// smartHTTPStub serves the minimal smart-HTTP surface git ls-remote needs:
// the ref advertisement with an empty ref list. It records the Authorization
// header it saw so the test can prove the token arrived over the wire.
type smartHTTPStub struct {
	t             *testing.T
	username      string
	token         string
	sawAuth       string
	advertiseFail bool
}

func (s *smartHTTPStub) handler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/info/refs") || r.URL.Query().Get("service") != "git-upload-pack" {
		http.NotFound(w, r)
		return
	}
	s.sawAuth = r.Header.Get("Authorization")
	if s.advertiseFail {
		w.Header().Set("WWW-Authenticate", `Basic realm="gotham-test"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.username+":"+s.token))
	if s.sawAuth != want {
		w.Header().Set("WWW-Authenticate", `Basic realm="gotham-test"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	_, _ = w.Write([]byte("001e# service=git-upload-pack\n0000" + "0000"))
}

// TestConnectionProbeHTTPSAgainstSmartHTTPStub runs the production probe
// (TestGitConnection with the real git binary) against a local smart-HTTP
// stub: the success case proves the stored token reaches the remote as a
// Basic credential, and the 401 case proves the classified auth hint. No
// external network is involved.
func TestConnectionProbeHTTPSAgainstSmartHTTPStub(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary is not available")
	}
	_ = git

	stub := &smartHTTPStub{t: t, username: "bob", token: "s3cr3t-token"}
	server := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer server.Close()

	userID := uuid.New()
	app := privateTestApp(userID, server.URL+"/repo.git")
	app.Branch = "main"
	repo := &fakeRepository{app: app}
	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })

	if _, err := svc.SetGitCredential(context.Background(), userID, app.ID, "bob", "s3cr3t-token"); err != nil {
		t.Fatalf("SetGitCredential: %v", err)
	}
	result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("TestGitConnection: %v", err)
	}
	if !result.OK {
		t.Fatalf("result = %+v, want a successful probe", result)
	}
	if result.Host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", result.Host)
	}
	if stub.sawAuth == "" {
		t.Error("the stub never saw an Authorization header: the token was not injected")
	}
	if strings.Contains(result.Message, "s3cr3t-token") {
		t.Errorf("the probe message leaked the token: %q", result.Message)
	}

	t.Run("unauthorized is classified", func(t *testing.T) {
		stub.advertiseFail = true
		result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("TestGitConnection: %v", err)
		}
		if result.OK {
			t.Fatalf("result = %+v, want a failed probe", result)
		}
		if !strings.Contains(result.Message, "authentication is required") {
			t.Errorf("message = %q, want the auth hint", result.Message)
		}
		if strings.Contains(result.Message, "s3cr3t-token") {
			t.Errorf("the probe message leaked the token: %q", result.Message)
		}
	})
}

// serviceWithRunner builds a Service whose connection probe runs through the
// fake runner instead of the git binary.
func serviceWithRunner(t *testing.T, repo *fakeRepository, run cloneRunner) *Service {
	t.Helper()
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Source:     gitSource{keys: repo, creds: repo, run: run},
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// TestTestGitConnectionOutcomes pins the probe contract without a network:
// a keyless SSH remote is an outcome (not an error) naming the deploy key,
// a git failure is classified, and a git echo of a token-bearing URL is
// scrubbed before it reaches the message.
func TestTestGitConnectionOutcomes(t *testing.T) {
	t.Run("keyless SSH names the deploy key", func(t *testing.T) {
		userID := uuid.New()
		app := privateTestApp(userID, "git@git.internal:acme/demo.git")
		repo := &fakeRepository{app: app}
		ran := false
		svc := serviceWithRunner(t, repo, func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		})
		result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("TestGitConnection: %v", err)
		}
		if result.OK {
			t.Errorf("result = %+v, want a failed probe", result)
		}
		if !strings.Contains(result.Message, "deploy key") {
			t.Errorf("message = %q, want it to name the deploy key", result.Message)
		}
		if result.Host != "git.internal" {
			t.Errorf("host = %q, want git.internal", result.Host)
		}
		if ran {
			t.Error("git ran for a keyless SSH remote")
		}
	})

	t.Run("auth failure is classified", func(t *testing.T) {
		userID := uuid.New()
		app := privateTestApp(userID, "https://git.internal/acme/demo.git")
		repo := &fakeRepository{app: app}
		svc := serviceWithRunner(t, repo, func(context.Context, []string, []string) ([]byte, error) {
			return []byte("fatal: Authentication failed for 'https://git.internal/acme/demo.git/'\n"), errors.New("exit status 128")
		})
		result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("TestGitConnection: %v", err)
		}
		if result.OK {
			t.Errorf("result = %+v, want a failed probe", result)
		}
		if !strings.Contains(result.Message, "authentication is required") {
			t.Errorf("message = %q, want the auth hint", result.Message)
		}
	})

	t.Run("token-bearing echo is scrubbed", func(t *testing.T) {
		userID := uuid.New()
		app := privateTestApp(userID, "https://git.internal/acme/demo.git")
		repo := &fakeRepository{app: app}
		svc := serviceWithRunner(t, repo, func(context.Context, []string, []string) ([]byte, error) {
			return []byte("fatal: unable to access 'https://oauth2:glpat-S3CR3T@git.internal/acme/demo.git/': Failed\n"), errors.New("exit status 128")
		})
		result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("TestGitConnection: %v", err)
		}
		if strings.Contains(result.Message, "glpat-S3CR3T") {
			t.Errorf("message leaked the echoed token: %q", result.Message)
		}
		if !strings.Contains(result.Message, "***@") {
			t.Errorf("message = %q, want the scrubbed userinfo", result.Message)
		}
	})

	t.Run("non-private source is refused", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		svc := serviceWithRunner(t, repo, nil)
		if _, err := svc.TestGitConnection(context.Background(), userID, app.ID); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		app := privateTestApp(userID, "https://git.internal/acme/demo.git")
		repo := &fakeRepository{app: app}
		var gotArgv []string
		svc := serviceWithRunner(t, repo, func(_ context.Context, argv []string, _ []string) ([]byte, error) {
			gotArgv = append([]string(nil), argv...)
			return []byte("deadbeef\tHEAD\n"), nil
		})
		result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("TestGitConnection: %v", err)
		}
		if !result.OK {
			t.Errorf("result = %+v, want success", result)
		}
		if len(gotArgv) < 2 || gotArgv[0] != "git" || gotArgv[1] != "ls-remote" {
			t.Errorf("argv = %v, want a git ls-remote probe", gotArgv)
		}
	})
}

// TestRoutesGetDeployKey pins the read path the detail page uses: the
// public half with a 200, a 404 without a key, and a body that never names
// a private half or a token.
func TestRoutesGetDeployKey(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()

	t.Run("public half only", func(t *testing.T) {
		svc := &fakeDeployService{deployKey: DeployKey{
			ID:            uuid.New(),
			ApplicationID: appID,
			Repo:          "git@git.internal:acme/demo.git",
			Fingerprint:   "SHA256:abc",
			PublicKey:     "ssh-ed25519 AAAA gotham:deploy:" + appID.String(),
		}}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, deployKeyPath(appID), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var body deployKeyEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.DeployKey.PublicKey == "" || body.DeployKey.Fingerprint != "SHA256:abc" {
			t.Errorf("body = %+v, want the public half", body.DeployKey)
		}
		for _, secret := range []string{"private", "token", "ciphertext", "sealed"} {
			if strings.Contains(strings.ToLower(rec.Body.String()), secret) {
				t.Errorf("response names %q: %s", secret, rec.Body.String())
			}
		}
	})

	t.Run("application without a key", func(t *testing.T) {
		svc := &fakeDeployService{getKeyErr: ErrNotFound}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, deployKeyPath(appID), nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
		}
	})
}

func gitCredentialPath(appID uuid.UUID) string {
	return "/v1/applications/" + appID.String() + "/git-credential"
}

func testConnectionPath(appID uuid.UUID) string {
	return "/v1/applications/" + appID.String() + "/test-connection"
}

// TestRoutesGitCredential pins the HTTPS credential endpoints: storing
// answers what is set (never the token), reading reports the state, and a
// missing token is a 400.
func TestRoutesGitCredential(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()

	t.Run("store and read", func(t *testing.T) {
		svc := &fakeDeployService{gitCredState: GitCredentialState{HasCredential: true, Username: "bob"}}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, gitCredentialPath(appID),
			strings.NewReader(`{"username":"bob","token":"s3cr3t"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("put status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var stored gitCredentialResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stored); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !stored.HasCredential || stored.Username != "bob" {
			t.Errorf("body = %+v, want the stored state", stored)
		}
		if strings.Contains(rec.Body.String(), "s3cr3t") {
			t.Errorf("response echoed the token: %s", rec.Body.String())
		}
		if svc.seenGitUsername != "bob" || svc.seenGitToken != "s3cr3t" {
			t.Errorf("service saw %q/%q, want bob/s3cr3t", svc.seenGitUsername, svc.seenGitToken)
		}

		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, gitCredentialPath(appID), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("get status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("validation is a 400", func(t *testing.T) {
		svc := &fakeDeployService{gitCredErr: ErrValidation}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, gitCredentialPath(appID),
			strings.NewReader(`{"username":"bob","token":""}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
		}
	})
}

// TestRoutesTestConnection pins the probe endpoint: the classified outcome
// passes through with a 200 (even when the probe failed), while a service
// failure answers 500 without its detail.
func TestRoutesTestConnection(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()

	t.Run("outcome passes through", func(t *testing.T) {
		for _, outcome := range []GitConnectionResult{
			{OK: true, Message: "connection succeeded", Host: "git.internal"},
			{OK: false, Message: "git ls-remote failed (authentication is required)", Host: "git.internal"},
		} {
			svc := &fakeDeployService{connResult: outcome}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, testConnectionPath(appID), nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			var body gitConnectionResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.OK != outcome.OK || body.Message != outcome.Message || body.Host != outcome.Host {
				t.Errorf("body = %+v, want %+v", body, outcome)
			}
		}
	})

	t.Run("service failure stays internal", func(t *testing.T) {
		svc := &fakeDeployService{connErr: errors.New("boom")}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, testConnectionPath(appID), nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
		}
		if containsMessage(rec.Body.String(), "boom") {
			t.Errorf("response leaked an internal error: %s", rec.Body.String())
		}
	})
}

// TestPreviewOfPrivateAppCopiesLocalKey pins the preview flow for a
// provider-less private application: the preview inherits the type and the
// sibling gets its own mapping row over the same key material.
func TestPreviewOfPrivateAppCopiesLocalKey(t *testing.T) {
	userID := uuid.New()
	base := privateTestApp(userID, "git@git.internal:acme/demo.git")
	base.TeamID = uuid.New()
	repo := seedBaseForPreview(t, base)
	svc := newTestService(t, repo)

	key, err := svc.CreateDeployKey(context.Background(), userID, base.ID)
	if err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}
	created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name:   "demo app-pr-1",
		Branch: "feat/x",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if created.SourceType != SourceGitPrivate || created.Provider != "" {
		t.Errorf("preview source = (%q, %q), want (git_private, \"\")",
			created.SourceType, created.Provider)
	}
	sibling, err := repo.GetDeployKey(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("preview deploy key: %v", err)
	}
	if sibling.PublicKey != key.PublicKey || sibling.ProviderKeyID != "" {
		t.Errorf("sibling key = %+v, want the base public half with no provider key", sibling)
	}
}
