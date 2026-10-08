package deploy

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// staticTokenResolver hands the cloner a fixed installation token without a
// GitHub App.
type staticTokenResolver struct {
	token string
	err   error
	calls int
}

// Compile-time guarantee that staticTokenResolver satisfies the seam.
var _ appTokenResolver = (*staticTokenResolver)(nil)

// InstallationToken implements appTokenResolver.
func (r *staticTokenResolver) InstallationToken(_ context.Context, _, _ uuid.UUID) (string, error) {
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	return r.token, nil
}

// TestGitSourceCloneGitHubAppUsesToken proves a github_app application clones
// with a fresh installation token: the token lands in the clone URL userinfo,
// the log line carries the redacted URL, and the stored clone URL is never
// rewritten with the token. The stored URL is the wizard's https clone_url
// (private repos included): the token cloner only accepts http(s).
func TestGitSourceCloneGitHubAppUsesToken(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceGitHubApp
	app.CloneURL = "https://github.com/acme/private-web.git"
	dir := filepath.Join(t.TempDir(), "repo")

	var gotArgv []string
	var logged []string
	run := func(_ context.Context, argv, _ []string) ([]byte, error) {
		gotArgv = append([]string(nil), argv...)
		return nil, nil
	}
	resolver := &staticTokenResolver{token: "fresh-install-token"}
	source := gitSource{keys: &staticKeyResolver{}, appTokens: resolver, run: run}

	if err := source.Clone(context.Background(), app, dir, func(line string) {
		logged = append(logged, line)
	}); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("token calls = %d, want 1 (fresh per attempt)", resolver.calls)
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
	if err := (gitSource{appTokens: &staticTokenResolver{token: "  "}, run: run}).Clone(context.Background(), app, dir, nil); err == nil {
		t.Error("clone with an empty token succeeded")
	}
}

// TestTokenCloneURLShapes pins the token URL builder: https only, userinfo
// replaced, anything else refused.
func TestTokenCloneURLShapes(t *testing.T) {
	got, err := tokenCloneURL("https://github.com/acme/demo.git", "tok")
	if err != nil {
		t.Fatalf("tokenCloneURL: %v", err)
	}
	if got != "https://x-access-token:tok@github.com/acme/demo.git" {
		t.Errorf("tokenCloneURL = %q", got)
	}
	for _, raw := range []string{
		"git@github.com:acme/demo.git",
		"ssh://git@github.com/acme/demo.git",
		"/srv/repos/demo",
		"https://user:old@github.com/acme/demo.git",
	} {
		got, err := tokenCloneURL(raw, "tok")
		if raw == "https://user:old@github.com/acme/demo.git" {
			if err != nil || got != "https://x-access-token:tok@github.com/acme/demo.git" {
				t.Errorf("tokenCloneURL(%q) = %q, %v", raw, got, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("tokenCloneURL(%q) = %q, want an error", raw, got)
		}
	}
}
