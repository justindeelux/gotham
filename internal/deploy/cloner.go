package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/githubapp"
)

// Source prepares the build tree for a deployment: it checks the
// application's repository out into dir. Production shells out to git;
// tests substitute a fake that writes a fixture tree.
type Source interface {
	Clone(ctx context.Context, app Application, dir string, log func(string)) error
}

// deployKeyResolver is the slice of Repository the cloner needs: the sealed
// deploy private key of one application. It is an interface so tests can hand
// the cloner a fixed key without a database.
type deployKeyResolver interface {
	// DeployKeyPrivatePEM opens an application's deploy key. An application
	// without one answers "" and no error — that is what keeps anonymous
	// cloning the default.
	DeployKeyPrivatePEM(ctx context.Context, appID uuid.UUID) (string, error)
}

// appTokenResolver builds the token-authenticated clone URL for a github_app
// application. Resolution is by repository grant (not by row id): the token
// is minted for the installation that grants the repo, embedded only in the
// returned URL, never persisted and never logged by callers.
type appTokenResolver interface {
	// TokenCloneURL returns cloneURL with a fresh installation token for the
	// installation granting repo and owned by userID.
	TokenCloneURL(ctx context.Context, userID uuid.UUID, repo, cloneURL string) (string, error)
}

// cloneRunner executes git with an environment and returns its combined
// output. Tests substitute one to assert the command line, the environment
// and the ephemeral key file without a network or an sshd.
type cloneRunner func(ctx context.Context, argv []string, env []string) ([]byte, error)

// gitSource clones over HTTPS/SSH with the git binary on the control plane.
// The result is a shallow, single-branch working tree that the build engines
// consume directly (they exclude .git from the context themselves).
//
// An application that holds a deploy key is cloned through GIT_SSH_COMMAND
// with an ephemeral 0600 key file; an application without one keeps the
// anonymous clone it has always had.
type gitSource struct {
	// keys opens the application's deploy private key; nil disables key
	// lookup entirely (anonymous clone).
	keys deployKeyResolver
	// appTokens builds token-authenticated clone URLs for linked github_app
	// sources; nil keeps every clone on the legacy path.
	appTokens appTokenResolver
	// run executes git; nil selects the real binary.
	run cloneRunner
	// logger records host-key trust downgrades; nil selects slog.Default.
	logger *slog.Logger
}

// Compile-time guarantee that gitSource satisfies the Source seam.
var _ Source = gitSource{}

// defaultBranch is used when an application carries no branch (older rows or
// a provider that reported an empty default).
const defaultBranch = "main"

// Clone implements Source. The clone URL is passed after "--" so a hostile
// value can never be parsed as an option, and only the known git URL shapes
// are accepted.
func (s gitSource) Clone(ctx context.Context, app Application, dir string, log func(string)) error {
	url := strings.TrimSpace(app.CloneURL)
	if err := validateCloneURL(url); err != nil {
		return err
	}
	// tokenClone marks the installation-token path for the log line: the
	// token itself never appears (see RedactCloneURL).
	tokenClone := false
	// The token path is explicit: the application must be linked to a GitHub
	// App connection AND carry an http(s) clone URL. Anything else (legacy
	// provider=github rows, deploy keys, SSH URLs, local fixtures) never
	// touches the resolver, so no GitHub API call happens for it.
	if app.GitHubAppID != uuid.Nil && s.appTokens != nil && isHTTPCloneURL(url) {
		tokenURL, err := s.appTokens.TokenCloneURL(ctx, app.UserID, app.Repo, url)
		if err == nil {
			url = tokenURL
			tokenClone = true
		} else if errors.Is(err, githubapp.ErrNoInstallationGrant) {
			// The link exists but no installation grants the repo (revoked
			// grant): the legacy path still applies, visibly. Anything
			// else (host mismatch, mint failure, unverifiable grants)
			// fails the clone instead of silently cloning anonymously.
			if log != nil {
				log(fmt.Sprintf("no GitHub App installation grants %q; cloning without an installation token", app.Repo))
			}
		} else {
			return err
		}
	}
	privatePEM, err := s.deployKeyPEM(ctx, app.ID)
	if err != nil {
		return err
	}
	if privatePEM == "" && isSSHTransportURL(url) {
		// A keyless clone runs ssh with the control plane's ambient
		// identity: refuse, so an SSH URL on a credential-less source is a
		// clear validation error instead of an authentication surprise.
		return fmt.Errorf("%w: SSH clone URL %q needs a deploy key: use a public http(s) or git URL, or a private-git source",
			ErrValidation, RedactCloneURL(url))
	}
	if privatePEM != "" && !tokenClone {
		// A deploy key is an SSH credential: over http(s) it would
		// authenticate nothing, so an http(s) URL is rewritten to its SSH
		// shape first. Skipped on the token path: the token already
		// authenticates the https URL, and rewriting would drop it while
		// the log line claims token use. This is the fallback for rows that
		// still carry an https URL (API clients, applications created
		// before BE-4.4b); the wizard stores the provider's own ssh_url for
		// private repositories, which passes through with its port intact.
		url = sshCloneURL(url)
	}
	branch := strings.TrimSpace(app.Branch)
	// A retried step re-enters Clone with the same directory: clear whatever
	// the previous attempt left behind so git does not refuse a non-empty target.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("git clone: clear %s: %w", dir, err)
	}

	// The child environment must be deterministic regardless of the ambient
	// one: a CI runner (actions/checkout) or a developer shell may export
	// GIT_SSH_COMMAND, and git would then use it for an anonymous clone or
	// combine it with the deploy key one. Drop every inherited entry first;
	// the keyed branch below adds back exactly one.
	env := withoutEnv(os.Environ(), "GIT_SSH_COMMAND")
	if privatePEM != "" {
		if devAcceptNewHostKeys() {
			// A stray value in the service environment silently downgrades
			// every keyed clone; make it visible on each clone.
			s.log().Warn("deploy: keyed clone trusts unseen host keys",
				"reason", devAcceptNewHostKeysEnv+" is set",
				"risk", "the clone accepts any host key; disable this in production",
			)
		}
		files, err := newDeployKeyFiles(privatePEM, s.log())
		if err != nil {
			return err
		}
		defer files.remove()
		env = files.sshEnv(env)
	}

	// An empty branch pins the remote's default via ls-remote (GS-3) instead
	// of guessing "main". It runs with the clone environment, so a keyed SSH
	// remote resolves through the same deploy key.
	runner := s.run
	if runner == nil {
		runner = runGit
	}
	if branch == "" {
		resolved, err := defaultBranchFor(ctx, runner, url, env)
		if err != nil {
			return err
		}
		branch = resolved
	}

	if log != nil {
		line := fmt.Sprintf("git clone --depth 1 --branch %s %s", branch, RedactCloneURL(url))
		if tokenClone {
			line += " (using a fresh installation token)"
		} else if privatePEM != "" {
			line += " (using the application deploy key)"
		}
		log(line)
	}
	argv := []string{"git", "clone",
		"--depth", "1", "--single-branch", "--branch", branch, "--", url, dir}
	output, err := runner(ctx, argv, env)
	if ctx.Err() != nil {
		return fmt.Errorf("git clone: %w", ctx.Err())
	}
	if err != nil {
		quoted := tail(redactCloneError(string(output)), 400)
		if hint := classifyGitFailure(string(output)); hint != "" {
			return fmt.Errorf("git clone: %w: %s%s", err, quoted, hint)
		}
		return fmt.Errorf("git clone: %w: %s", err, quoted)
	}
	if log != nil {
		log("repository cloned (" + branch + ")")
	}
	return nil
}

// log returns the cloner's logger, defaulting to slog.Default.
func (s gitSource) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// deployKeyPEM resolves the application's deploy private key, turning a
// lookup failure into a clear clone error. Silently falling back to an
// anonymous clone would hide the problem behind an auth failure from the Git
// host on a private repository.
func (s gitSource) deployKeyPEM(ctx context.Context, appID uuid.UUID) (string, error) {
	if s.keys == nil {
		return "", nil
	}
	privatePEM, err := s.keys.DeployKeyPrivatePEM(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("deploy: load deploy key: %w", err)
	}
	return strings.TrimSpace(privatePEM), nil
}

// gitWaitDelay bounds how long Wait waits on the command's I/O pipes after the
// context is cancelled, so a process that inherited the pipes cannot outlive
// the cancellation and hold the deploy step open.
const gitWaitDelay = 10 * time.Second

// runGit executes argv (starting with "git") with env and returns its combined
// output, preserving exec.CommandContext's cancellation behaviour. The child is
// isolated (own process group, and on Linux a parent-death signal) so a
// cancelled clone takes down git and the ssh/sh it spawned; see
// configureGitCommand.
func runGit(ctx context.Context, argv []string, env []string) ([]byte, error) {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Env = env
	cleanup := configureGitCommand(command)
	defer cleanup()
	return command.CombinedOutput()
}

// deployKeyFiles is the per-deployment materialisation of a deploy private
// key: one 0600 file inside a fresh private directory, removed as soon as the
// clone returns. The key is never written anywhere else and never logged. The
// directory also holds the pinned known_hosts the clone verifies against.
type deployKeyFiles struct {
	dir            string
	keyPath        string
	knownHostsPath string
}

// newDeployKeyFiles writes the PEM private key into a fresh private directory
// (0700 by os.MkdirTemp) and returns it together with the pinned known_hosts
// materialised next to it.
func newDeployKeyFiles(privatePEM string, logger *slog.Logger) (*deployKeyFiles, error) {
	dir, err := os.MkdirTemp("", "gotham-deploy-key-*")
	if err != nil {
		return nil, fmt.Errorf("deploy: deploy key workspace: %w", err)
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, []byte(privatePEM), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("deploy: write deploy key: %w", err)
	}
	// WriteFile only applies its mode when it creates the file; chmod keeps
	// the guarantee explicit for every path (a re-used file must never keep a
	// wider mode).
	if err := os.Chmod(keyPath, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("deploy: protect deploy key: %w", err)
	}
	knownHostsPath, err := materializeKnownHosts(dir, logger)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &deployKeyFiles{dir: dir, keyPath: keyPath, knownHostsPath: knownHostsPath}, nil
}

// remove deletes the ephemeral key and the known_hosts materialised with it.
func (f *deployKeyFiles) remove() {
	_ = os.RemoveAll(f.dir)
}

// withoutEnv returns env with every entry naming the variable dropped. Git
// reads GIT_SSH_COMMAND from the child environment, so an inherited value must
// be removed before the cloner decides which one (if any) the clone uses.
func withoutEnv(env []string, name string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, name+"=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// sshEnv returns env plus GIT_SSH_COMMAND: ssh offering only this key
// (IdentitiesOnly), verifying against the pinned ephemeral known_hosts, and
// refusing an unknown host key (StrictHostKeyChecking=yes) unless the explicit
// dev flag re-enables accept-new — the control plane neither touches ~/.ssh nor
// prompts during a clone.
func (f *deployKeyFiles) sshEnv(env []string) []string {
	// GlobalKnownHostsFile=/dev/null ignores the host-wide
	// /etc/ssh/ssh_known_hosts and KnownHostsCommand, so the ephemeral pinned
	// set (embedded + GOTHAM_KNOWN_HOSTS) is the only trust anchor.
	command := "ssh -i " + shellQuote(f.keyPath) +
		" -o IdentitiesOnly=yes" +
		" -o UserKnownHostsFile=" + shellQuote(f.knownHostsPath) +
		" -o GlobalKnownHostsFile=/dev/null" +
		" -o StrictHostKeyChecking=" + strictHostKeyChecking(devAcceptNewHostKeys())
	return append(env, "GIT_SSH_COMMAND="+command)
}

// shellQuote wraps a path for the shell git runs GIT_SSH_COMMAND through
// (`sh -c`): single quotes keep spaces intact and a lone quote is escaped the
// POSIX way.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// sshCloneURL rewrites an http(s) clone URL into its SSH equivalent
// (https://host/owner/repo.git → ssh://git@host/owner/repo.git). Every other
// shape — ssh://, scp-like git@host:path, and development-local paths — is
// returned unchanged.
//
// The rewrite is the compatibility path for a deploy key on an https row: the
// FE stores the provider's own ssh_url for private repositories (BE-4.4b), so
// the URL the cloner uses is normally already authoritative. For an https row
// the port is dropped because a web port does not identify an SSH port (a
// self-hosted instance is often reached on 443 through a proxy while sshd
// listens on 22); an application that needs a non-default SSH port stores an
// ssh:// clone URL of its own, which passes through verbatim.
func sshCloneURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return raw
	}
	if parsed.Hostname() == "" {
		return raw
	}
	return (&url.URL{
		Scheme: "ssh",
		User:   url.User("git"),
		Host:   parsed.Hostname(),
		Path:   parsed.Path,
	}).String()
}

// devLocalCloneEnv re-enables local directories and file:// URLs as clone
// sources. Production keeps the allow-list remote-only so an application
// cannot point the control plane at its own filesystem; the flag exists for
// development fixtures (and the cloner tests).
const devLocalCloneEnv = "GOTHAM_DEV_CLONE_LOCAL"

// devLocalClone reports whether local clone sources are allowed.
func devLocalClone() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(devLocalCloneEnv)), "true")
}

// validateCloneURL rejects values git would refuse anyway, so the deploy log
// reports a clear validation error instead of a raw git usage message. Local
// paths and file:// URLs are only accepted when GOTHAM_DEV_CLONE_LOCAL=true;
// production allows remote http(s)/ssh/git URLs and scp-like git@host:path.
func validateCloneURL(url string) error {
	if url == "" {
		return fmt.Errorf("%w: application has no clone URL", ErrValidation)
	}
	switch {
	case strings.Contains(url, "://"):
		if !hasAllowedScheme(url) {
			return fmt.Errorf("%w: unsupported clone URL scheme", ErrValidation)
		}
		return nil
	case strings.HasPrefix(url, "git@"):
		return nil
	case strings.HasPrefix(url, "/"):
		if !devLocalClone() {
			return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported clone URL", ErrValidation)
	}
}

// isHTTPCloneURL reports whether raw uses an http(s) scheme: the only shape
// that can carry an installation token.
func isHTTPCloneURL(raw string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return false
	}
	switch strings.ToLower(scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

// hasAllowedScheme reports whether the URL uses a scheme git can clone from.
func hasAllowedScheme(url string) bool {
	scheme, _, ok := strings.Cut(url, "://")
	if !ok {
		return false
	}
	switch strings.ToLower(scheme) {
	case "http", "https", "ssh", "git":
		return true
	case "file":
		return devLocalClone()
	default:
		return false
	}
}

// tail returns the last n bytes of s, used to quote a failing command's
// output without dumping a whole build log into the error message.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// RedactCloneURL removes the userinfo from a clone URL before it is logged
// or returned to API clients: an operator-supplied URL may carry a token
// (https://x-access-token:ghp_…@host/repo) and the realtime deploy log is
// visible to the application's team. It never returns a credential-bearing
// string raw: a URL url.Parse rejects falls back to the regex redaction, and a
// scp-like git@host:path (no userinfo to strip) is returned unchanged. The one
// bound is whitespace, which cannot be part of a userinfo a git/curl client
// could use (see userinfoPattern).
func RedactCloneURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		// An unparseable URL can still be logged verbatim by callers, so
		// redact it by pattern rather than trusting Parse to have validated
		// it. Only a string with an authority can carry userinfo.
		if strings.Contains(raw, "://") {
			return redactCloneError(raw)
		}
		return raw
	}
	if parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

// redactCloneError strips embedded credentials from a quoted git error tail:
// some git versions echo the URL they were handed, so the token could reach
// the deploy log through stderr even though the command line never logs it.
// It runs on the untruncated output (see Clone), so a cut inside the userinfo
// can never expose the remainder.
func redactCloneError(msg string) string {
	return userinfoPattern.ReplaceAllString(msg, "$1***@")
}

// userinfoPattern matches the credential half of a URL that carries one:
// <scheme>://<user>:<password>@. The password is optional (a bare user or a
// token-as-user must be hidden too). The negated class excludes only
// whitespace: it crosses both a literal '/' and a literal '@' in the password,
// so the match runs to the LAST '@' in the whitespace-delimited token and
// neither can leave a suffix behind. It is best effort — a hostile URL is never
// trusted, only hidden. Userinfo containing whitespace cannot be matched
// without swallowing unrelated diagnostic text, and such a URL is rejected by
// git/curl, so it cannot carry a working credential. A credential-free message
// is never mangled beyond hiding a userinfo-shaped token: a bare '@' in a path
// or query (https://example.com/~user/repo@v2.git) is accepted over-redaction,
// which is safe — see TestRedactCloneURLAndError.
var userinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s]+@`)
