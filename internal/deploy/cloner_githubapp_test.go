package deploy

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/githubapp"
)

// staticTokenResolver hands the cloner a fixed token clone URL without a
// GitHub App, recording what it was asked for.
type staticTokenResolver struct {
	tokenURL string
	err      error
	calls    int
	gotAppID uuid.UUID
	gotRepo  string
	gotURL   string
}

// Compile-time guarantee that staticTokenResolver satisfies the seam.
var _ appTokenResolver = (*staticTokenResolver)(nil)

// TokenCloneURL implements appTokenResolver.
func (r *staticTokenResolver) TokenCloneURL(_ context.Context, _ uuid.UUID, appID uuid.UUID, repo, cloneURL string) (string, error) {
	r.calls++
	r.gotAppID = appID
	r.gotRepo = repo
	r.gotURL = cloneURL
	if r.err != nil {
		return "", r.err
	}
	return r.tokenURL, nil
}

// TestGitSourceCloneGitHubAppUsesToken proves a github_app application clones
// with a fresh installation token: the token lands in the clone URL userinfo,
// the log line carries the redacted URL, and the stored clone URL is never
// rewritten with the token. The stored URL is the wizard's https clone_url
// (private repos included): the token cloner only accepts http(s).
func TestGitSourceCloneGitHubAppUsesToken(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	app.GitHubAppID = uuid.New()
	app.Repo = "acme/private-web"
	app.CloneURL = "https://github.com/acme/private-web.git"
	dir := filepath.Join(t.TempDir(), "repo")

	var gotArgv []string
	var logged []string
	run := func(_ context.Context, argv, _ []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		return nil, nil
	}
	resolver := &staticTokenResolver{tokenURL: "https://x-access-token:fresh-install-token@github.com/acme/private-web.git"}
	source := gitSource{keys: &staticKeyResolver{}, appTokens: resolver, run: run}

	if err := source.Clone(context.Background(), app, dir, func(line string) {
		logged = append(logged, line)
	}); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("token calls = %d, want 1 (fresh per attempt)", resolver.calls)
	}
	// The resolver receives the linked connection, the application repo and
	// stored URL: that is what lets the real service resolve the granting
	// installation of that connection instead of assuming one.
	if resolver.gotAppID != app.GitHubAppID {
		t.Errorf("resolver app id = %v, want the linked connection %v", resolver.gotAppID, app.GitHubAppID)
	}
	if resolver.gotRepo != "acme/private-web" {
		t.Errorf("resolver repo = %q, want the application repo", resolver.gotRepo)
	}
	if resolver.gotURL != "https://github.com/acme/private-web.git" {
		t.Errorf("resolver url = %q, want the stored clone URL", resolver.gotURL)
	}
	joined := strings.Join(gotArgv, " ")
	if !strings.Contains(joined, "https://x-access-token:fresh-install-token@github.com/acme/private-web.git") {
		t.Errorf("argv = %v, want the token clone URL", gotArgv)
	}
	for _, line := range logged {
		if strings.Contains(line, "fresh-install-token") {
			t.Errorf("log line leaks the token: %q", line)
		}
	}
	if !strings.Contains(strings.Join(logged, "\n"), "installation token") {
		t.Errorf("log lines = %v, want the installation-token marker", logged)
	}
	if app.CloneURL != "https://github.com/acme/private-web.git" {
		t.Errorf("stored clone URL = %q, want it unchanged (token never persisted)", app.CloneURL)
	}
}

// TestGitSourceCloneGitHubAppWithoutResolverKeepsLegacy proves a github_app
// clone without a token resolver keeps the legacy behaviour instead of
// failing: configurations without a GitHub App service (tests, custom
// wiring) clone exactly as before.
func TestGitSourceCloneGitHubAppWithoutResolverKeepsLegacy(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	var gotArgv []string
	source := gitSource{
		run: func(_ context.Context, argv, _ []string) ([]byte, error) {
			gotArgv = append([]string(nil), argv...)
			return nil, nil
		},
	}
	if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if joined := strings.Join(gotArgv, " "); !strings.Contains(joined, app.CloneURL) {
		t.Errorf("argv = %v, want the unchanged clone URL", gotArgv)
	}
}

// TestGitSourceCloneGitHubAppUnlinkedKeepsLegacy proves an unlinked
// application never touches the resolver: a provider=github application with
// an SSH clone URL and a deploy key clones through the deploy-key path
// exactly as before GitHub App connections existed.
func TestGitSourceCloneGitHubAppUnlinkedKeepsLegacy(t *testing.T) {
	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	app.Provider = "github"
	app.Repo = "acme/legacy"
	app.CloneURL = "git@github.com:acme/legacy.git"

	var gotArgv, gotEnv []string
	resolver := &staticTokenResolver{err: githubapp.ErrNoInstallationGrant}
	source := gitSource{
		keys:      &staticKeyResolver{pem: privatePEM},
		appTokens: resolver,
		run: func(_ context.Context, argv, env []string) ([]byte, error) {
			gotArgv = append([]string(nil), argv...)
			gotEnv = append([]string(nil), env...)
			return nil, nil
		},
	}
	if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if resolver.calls != 0 {
		t.Errorf("resolver calls = %d, want 0 for an unlinked application", resolver.calls)
	}
	joined := strings.Join(gotArgv, " ")
	if !strings.Contains(joined, "git@github.com:acme/legacy.git") {
		t.Errorf("argv = %v, want the unchanged SSH clone URL", gotArgv)
	}
	if strings.Contains(joined, "x-access-token") {
		t.Errorf("argv = %v, want no token without a link", gotArgv)
	}
	if !hasEntry(gotEnv, "GIT_SSH_COMMAND") {
		t.Errorf("env = %v, want the deploy-key SSH command", gotEnv)
	}
}

// TestGitSourceCloneLinkedWithoutGrantFails proves a linked application
// whose connection no longer grants the repo fails the deploy: no other
// connection is consulted and no anonymous clone is attempted.
func TestGitSourceCloneLinkedWithoutGrantFails(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	app.GitHubAppID = uuid.New()
	app.Repo = "acme/web"
	app.CloneURL = "https://github.com/acme/web.git"

	ran := false
	source := gitSource{
		appTokens: &staticTokenResolver{err: githubapp.ErrNoInstallationGrant},
		run: func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		},
	}
	err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), func(string) {})
	if err == nil {
		t.Fatal("clone without a grant on the linked connection succeeded")
	}
	if !errors.Is(err, githubapp.ErrNoInstallationGrant) {
		t.Fatalf("clone err = %v, want ErrNoInstallationGrant", err)
	}
	if ran {
		t.Error("git ran despite the missing grant: no anonymous clone may be attempted")
	}
}

// TestGitSourceCloneGitHubAppTokenFailureFails proves a token failure that is
// not "no grant" (host mismatch, mint failure) fails the clone instead of
// silently cloning anonymously.
func TestGitSourceCloneGitHubAppTokenFailureFails(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	app.GitHubAppID = uuid.New()
	app.CloneURL = "https://github.com/acme/web.git"
	source := gitSource{
		appTokens: &staticTokenResolver{err: errors.New("mint failed")},
		run:       func(context.Context, []string, []string) ([]byte, error) { return nil, nil },
	}
	if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err == nil {
		t.Error("clone with a token failure succeeded")
	}
}
