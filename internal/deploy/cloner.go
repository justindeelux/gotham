package deploy

import (
	"context"
	"fmt"
	"log/slog"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
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
// application through its own linked connection. The token is minted for the
// installation of that connection granting the repo, embedded only in the
// returned URL, never persisted and never logged by callers.
type appTokenResolver interface {
	// TokenCloneURL returns cloneURL with a fresh installation token for the
	// installation of appID granting repo and owned by userID.
	TokenCloneURL(ctx context.Context, userID, appID uuid.UUID, repo, cloneURL string) (string, error)
}

// cloneRunner executes git with an environment and returns its combined
// output. Tests substitute one to assert the command line, the environment
// and the ephemeral key file without a network or an sshd.
type cloneRunner func(ctx context.Context, argv []string, env []string) ([]byte, error)

// gitSource clones over HTTPS/SSH with the git binary on the control plane.
// The result is a shallow, single-branch working tree that the build engines
// consume directly (they exclude .git from the context themselves).
//
// An SSH remote is cloned through GIT_SSH_COMMAND with an ephemeral 0600 key
// file verified against the pinned known_hosts (StrictHostKeyChecking=yes,
// unless the documented dev flag re-enables accept-new); an HTTPS remote of
// a git_private source uses the stored token through an ephemeral GIT_ASKPASS
// helper, falling back to the deploy key (rewritten to SSH) and then to the
// anonymous clone it has always had.
type gitSource struct {
	// keys opens the application's deploy private key; nil disables key
	// lookup entirely (anonymous clone).
	keys deployKeyResolver
	// appTokens builds token-authenticated clone URLs for linked github_app
	// sources; nil keeps every clone on the legacy path.
	appTokens appTokenResolver
	// creds opens the application's HTTPS git credential; nil disables
	// credential lookup entirely (anonymous HTTPS clone).
	creds gitCredentialResolver
	// run executes git; nil selects the real binary.
	run cloneRunner
	// lookup resolves the remote host for the SSRF host policy (see
	// pinGitRemoteHost); nil selects defaultGitHostLookup.
	lookup gitHostLookupFunc
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
	// tokenClone marks the GS-5 installation-token path for the log line:
	// the token itself never appears (see RedactCloneURL).
	tokenClone := false
	// The token path is explicit: the application must be linked to a GitHub
	// App connection AND carry an http(s) clone URL. Anything else (legacy
	// provider=github rows, deploy keys, SSH URLs, local fixtures) never
	// touches the resolver, so no GitHub API call happens for it. The link
	// selects the connection: only its installations are consulted, and a
	// revoked grant fails the deploy instead of borrowing another
	// connection or silently cloning anonymously.
	if app.GitHubAppID != uuid.Nil && s.appTokens != nil && isHTTPCloneURL(url) {
		tokenURL, err := s.appTokens.TokenCloneURL(ctx, app.UserID, app.GitHubAppID, app.Repo, url)
		if err != nil {
			return err
		}
		url = tokenURL
		tokenClone = true
	} else if app.GitHubAppID == uuid.Nil && app.SourceType == SourceGitHubApp && isHTTPCloneURL(url) && log != nil {
		// No connection is linked: after a disconnect unlinks the row (ON
		// DELETE SET NULL) the application silently fell back to an
		// anonymous clone. Say so on the deploy log, visibly.
		log("no GitHub App connection is linked; cloning without an installation token")
	}
	var (
		env     []string
		cleanup func()
		using   string
	)
	if tokenClone {
		// The installation token already authenticates the https URL, so the
		// URL is never rewritten to SSH (that would drop the token) and the
		// GS-4 credential is never consulted (a linked github_app source
		// cannot hold one).
		var err error
		env, cleanup, err = s.legacyKeyEnv(ctx, app.ID)
		if err != nil {
			return err
		}
		defer cleanup()
		using = "installation-token"
	} else {
		var err error
		url, env, cleanup, using, err = s.cloneCredential(ctx, app, url)
		if err != nil {
			return err
		}
		defer cleanup()
	}
	// The clone dials an operator-supplied host: resolve, refuse blocked
	// addresses and pin the answer before git runs (see pinGitRemoteHost).
	// It runs on the final URL (after any SSH rewrite), so the pinned host
	// is the one git actually dials.
	pinnedEnv, err := pinGitRemoteHost(ctx, s.lookup, url, env)
	if err != nil {
		return err
	}
	env = pinnedEnv
	if using == "deploy-key" && devAcceptNewHostKeys() {
		// A stray value in the service environment silently downgrades
		// every keyed clone; make it visible on each clone.
		s.log().Warn("deploy: keyed clone trusts unseen host keys",
			"reason", devAcceptNewHostKeysEnv+" is set",
			"risk", "the clone accepts any host key; disable this in production",
		)
	}
	branch := strings.TrimSpace(app.Branch)
	// A retried step re-enters Clone with the same directory: clear whatever
	// the previous attempt left behind so git does not refuse a non-empty target.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("git clone: clear %s: %w", dir, err)
	}

	// An empty branch pins the remote's default via ls-remote (GS-3) instead
	// of guessing "main". It runs with the clone environment, so a keyed SSH
	// remote (or an HTTPS remote with a stored token) resolves through the
	// same credential.
	runner := s.runOrDefault()
	if branch == "" {
		resolved, err := defaultBranchFor(ctx, runner, url, env)
		if err != nil {
			return err
		}
		branch = resolved
	}

	if log != nil {
		line := fmt.Sprintf("git clone --depth 1 --branch %s %s", branch, RedactCloneURL(url))
		switch using {
		case "installation-token":
			line += " (using a fresh installation token)"
		case "deploy-key":
			line += " (using the application deploy key)"
		case "https-credential":
			line += " (using the stored HTTPS credential)"
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
		quoted := quoteGitOutput(string(output))
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

// legacyKeyEnv builds the child environment for a clone whose URL already
// carries its credential (the GS-5 installation token): the URL is used as
// is, and a deploy key, when present, still materializes for the host-key
// policy. It mirrors the pre-GS-4 keyed path, including the trust-downgrade
// warning.
func (s gitSource) legacyKeyEnv(ctx context.Context, appID uuid.UUID) (env []string, cleanup func(), err error) {
	cleanup = func() {}
	env = withoutEnv(os.Environ(), "GIT_SSH_COMMAND")
	privatePEM, err := s.deployKeyPEM(ctx, appID)
	if err != nil {
		return nil, cleanup, err
	}
	if privatePEM == "" {
		return env, cleanup, nil
	}
	if devAcceptNewHostKeys() {
		s.log().Warn("deploy: keyed clone trusts unseen host keys",
			"reason", devAcceptNewHostKeysEnv+" is set",
			"risk", "the clone accepts any host key; disable this in production",
		)
	}
	files, err := newDeployKeyFiles(privatePEM, s.log())
	if err != nil {
		return nil, cleanup, err
	}
	return files.sshEnv(env), files.remove, nil
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

// withoutEnvPrefix returns env with every entry whose name starts with the
// prefix dropped. Git reads numbered GIT_CONFIG_* variables from the child
// environment, so an inherited set must be removed before the askpass helper
// installs exactly its own.
func withoutEnvPrefix(env []string, prefix string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if name, _, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(name, prefix) {
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
// prompts during a clone. -F /dev/null keeps the service user's ~/.ssh/config
// (a ProxyCommand there would otherwise still apply) out of the clone.
func (f *deployKeyFiles) sshEnv(env []string) []string {
	// GlobalKnownHostsFile=/dev/null ignores the host-wide
	// /etc/ssh/ssh_known_hosts and KnownHostsCommand, so the ephemeral pinned
	// set (embedded + GOTHAM_KNOWN_HOSTS) is the only trust anchor.
	command := "ssh -F /dev/null -i " + shellQuote(f.keyPath) +
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
	parsed, err := neturl.Parse(raw)
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
	return (&neturl.URL{
		Scheme: "ssh",
		User:   neturl.User("git"),
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
// production allows remote http(s)/ssh/git URLs and scp-like [user@]host:path
// (the same SSH shape ValidatePrivateGitURL accepts, so a created private
// source always stays deployable).
func validateCloneURL(url string) error {
	if url == "" {
		return fmt.Errorf("%w: application has no clone URL", ErrValidation)
	}
	if err := rejectLeadingDash("clone URL", url); err != nil {
		return err
	}
	switch {
	case strings.Contains(url, "://"):
		if !hasAllowedScheme(url) {
			return fmt.Errorf("%w: unsupported clone URL scheme", ErrValidation)
		}
		if parsed, err := neturl.Parse(url); err == nil {
			if err := rejectLeadingDash("host", parsed.Hostname()); err != nil {
				return err
			}
			if user := parsed.User.Username(); user != "" {
				if err := rejectLeadingDash("user", user); err != nil {
					return err
				}
			}
			// The host policy judges the host, never an embedded credential:
			// the GS-5 installation-token URL carries userinfo internally.
			if parsed.Hostname() != "" && !strings.EqualFold(parsed.Scheme, "file") {
				if err := checkGitHostLiteral(parsed.Hostname()); err != nil {
					return err
				}
			}
		}
		return nil
	case isSSHTransportURL(url):
		if user, host := splitScpAuthority(url); user != "" || host != "" {
			if err := rejectLeadingDash("user", user); err != nil {
				return err
			}
			if err := rejectLeadingDash("host", host); err != nil {
				return err
			}
			if err := checkGitHostLiteral(host); err != nil {
				return err
			}
		}
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
// string raw: a URL neturl.Parse rejects falls back to the regex redaction, and a
// scp-like git@host:path (no userinfo to strip) is returned unchanged. The one
// bound is whitespace, which cannot be part of a userinfo a git/curl client
// could use (see userinfoPattern).
func RedactCloneURL(raw string) string {
	parsed, err := neturl.Parse(raw)
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
