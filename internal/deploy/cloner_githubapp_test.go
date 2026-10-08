package deploy

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// staticTokenResolver hands the cloner a fixed token clone URL without a
// GitHub App, recording what it was asked for.
type staticTokenResolver struct {
	tokenURL string
	err      error
	calls    int
	gotRepo  string
	gotURL   string
}

// Compile-time guarantee that staticTokenResolver satisfies the seam.
var _ appTokenResolver = (*staticTokenResolver)(nil)

// TokenCloneURL implements appTokenResolver.
func (r *staticTokenResolver) TokenCloneURL(_ context.Context, _ uuid.UUID, repo, cloneURL string) (string, error) {
	r.calls++
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
	// The resolver receives the application repo and stored URL, not opaque
	// ids: that is what lets the real service resolve the granting
	// installation instead of assuming one.
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

// TestGitSourceCloneGitHubAppFailsClosed proves a github_app clone without a
// token resolver (or with a token failure) errors instead of cloning
// anonymously.
func TestGitSourceCloneGitHubAppFailsClosed(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	dir := filepath.Join(t.TempDir(), "repo")
	run := func(context.Context, []string, []string) ([]byte, error) { return nil, nil }

	if err := (gitSource{run: run}).Clone(context.Background(), app, dir, nil); err == nil {
		t.Error("clone without a token resolver succeeded")
	}
	if err := (gitSource{appTokens: &staticTokenResolver{err: errors.New("no grant")}, run: run}).Clone(context.Background(), app, dir, nil); err == nil {
		t.Error("clone with a resolver failure succeeded")
	}
}
