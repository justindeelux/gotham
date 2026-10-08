package deploy

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// Private git sources (GS-4) deploy from a repository no provider connection
// covers: an SSH URL (ssh:// or scp-like [user@]host:path) authenticated with
// a per-application ed25519 deploy key the operator registers by hand, or an
// HTTPS URL authenticated with a stored token injected through an ephemeral
// GIT_ASKPASS helper. The token and the private key never appear in API
// responses, process arguments or logs.

// ValidatePrivateGitURL accepts the clone URLs a provider-less private-git
// source may carry: ssh://, scp-like [user@]host:path and http(s) with a
// host, and no embedded userinfo — a token-bearing URL is refused, because
// credentials travel sealed (the HTTPS token) or as a deploy key, never in
// the stored URL. A plain http:// URL stays valid for anonymous clones, but
// it can never carry the stored token (tokens are injected over TLS only);
// SetGitCredential refuses such applications. Local paths and file:// URLs
// exist only for development fixtures behind GOTHAM_DEV_CLONE_LOCAL.
func ValidatePrivateGitURL(raw string) error {
	url := strings.TrimSpace(raw)
	if url == "" {
		return fmt.Errorf("%w: application has no clone URL", ErrValidation)
	}
	if hasURLWhitespace(url) {
		return fmt.Errorf("%w: unsupported clone URL: must not contain whitespace", ErrValidation)
	}
	if err := rejectLeadingDash("clone URL", url); err != nil {
		return err
	}
	if !strings.Contains(url, "://") {
		if isSSHTransportURL(url) {
			user, host := splitScpAuthority(url)
			if err := rejectLeadingDash("user", user); err != nil {
				return err
			}
			return rejectLeadingDash("host", host)
		}
		if strings.HasPrefix(url, "/") {
			if devLocalClone() {
				return nil
			}
			return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
		}
		return fmt.Errorf("%w: unsupported clone URL: use ssh, scp-like or https", ErrValidation)
	}
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("%w: unsupported clone URL", ErrValidation)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Hostname() == "" {
			return fmt.Errorf("%w: clone URL has no host", ErrValidation)
		}
		if err := rejectLeadingDash("host", parsed.Hostname()); err != nil {
			return err
		}
		if parsed.User != nil {
			return fmt.Errorf("%w: clone URL must not embed credentials: store the token as the HTTPS credential instead", ErrValidation)
		}
		return nil
	case "ssh":
		if parsed.Hostname() == "" {
			return fmt.Errorf("%w: clone URL has no host", ErrValidation)
		}
		if err := rejectLeadingDash("host", parsed.Hostname()); err != nil {
			return err
		}
		if user := parsed.User.Username(); user != "" {
			if err := rejectLeadingDash("user", user); err != nil {
				return err
			}
		}
		if parsed.User != nil && strings.Contains(parsed.User.String(), ":") {
			return fmt.Errorf("%w: clone URL must not embed credentials", ErrValidation)
		}
		return nil
	case "file":
		if devLocalClone() {
			return nil
		}
		return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
	default:
		return fmt.Errorf("%w: unsupported clone URL scheme %q: use ssh, scp-like or https", ErrValidation, parsed.Scheme)
	}
}

// rejectLeadingDash refuses a URL part starting with "-": git and ssh would
// parse it as a command-line flag (option injection). Both validators and
// the probe share it, so creation, deploy and test agree.
func rejectLeadingDash(what, value string) error {
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%w: unsupported clone URL: %s must not start with '-'", ErrValidation, what)
	}
	return nil
}

// splitScpAuthority splits an scp-like [user@]host:path into its user and
// host ("", "" when the value carries a scheme and is not scp-like).
func splitScpAuthority(raw string) (user, host string) {
	if strings.Contains(raw, "://") {
		return "", ""
	}
	rest := raw
	if at := strings.LastIndex(rest, "@"); at > 0 {
		user = rest[:at]
		rest = rest[at+1:]
	}
	host, _, _ = strings.Cut(rest, ":")
	return user, host
}

// isHTTPSURL reports whether raw is an https clone URL (after trimming).
// Only TLS counts: the stored token is injected solely over https, never
// over cleartext http.
func isHTTPSURL(raw string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return false
	}
	return strings.EqualFold(scheme, "https")
}

// isPlainHTTPURL reports whether raw is a cleartext http clone URL. Such a
// URL stays cloneable anonymously, but it can never carry the stored token.
func isPlainHTTPURL(raw string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return false
	}
	return strings.EqualFold(scheme, "http")
}

// privateGitHost extracts the remote host of a private-git clone URL for the
// connection-test result, so an unknown-host-key failure names the host the
// operator must pin in GOTHAM_KNOWN_HOSTS. It answers "" when no host parses.
func privateGitHost(raw string) string {
	url := strings.TrimSpace(raw)
	if strings.Contains(url, "://") {
		if parsed, err := neturl.Parse(url); err == nil {
			return parsed.Hostname()
		}
		return ""
	}
	// Scp-like [user@]host:path: the host sits between the last "@" and ":".
	_, host := splitScpAuthority(url)
	if host == "" || strings.Contains(host, "/") || !strings.Contains(url, ":") {
		return ""
	}
	return host
}

// gitCredentialResolver is the slice of Repository the cloner and the
// connection test need for HTTPS sources: the sealed token of one
// application, opened. An application without one answers empty strings and
// no error — that is what keeps anonymous cloning the default.
type gitCredentialResolver interface {
	// GitCredential opens an application's HTTPS git credential.
	GitCredential(ctx context.Context, appID uuid.UUID) (username, token string, err error)
}

// GitCredential is an application's stored HTTPS git credential as the API
// sees it: whether one is set and the plaintext username. The sealed token
// never leaves the server.
type GitCredential struct {
	Username  string
	UpdatedAt time.Time
}

// GitCredentialState is the API view of an application's HTTPS credential.
type GitCredentialState struct {
	HasCredential bool
	Username      string
}

// GitConnectionResult is the classified outcome of POST
// /v1/applications/{id}/test-connection: a git ls-remote against the stored
// clone URL with the stored credential. A failed probe is still a 200 —
// only request problems (unknown application, unusable source) are errors.
type GitConnectionResult struct {
	OK      bool
	Message string
	// Host is the remote host, so an unknown-host-key failure tells the
	// operator which host to pin (see the host-key policy on gitSource).
	Host string
}

// gitTestTimeout bounds one connection probe. It is deliberately short: the
// probe runs on the request path behind the SPA's own timeout.
const gitTestTimeout = 30 * time.Second

// maxGitTokenLength bounds a stored HTTPS token. Tokens are short strings;
// anything larger is a mistake, not a credential.
const maxGitTokenLength = 8192

// askpassFiles is the per-operation materialisation of an HTTPS credential:
// one 0700 shell script inside a fresh private directory, removed as soon as
// the git command returns. The token lives only in that file — never in the
// clone URL, process arguments, environment values or logs. The helper
// answers only prompts naming the expected host and refuses everything else,
// so a redirect to another host can never spend the token there.
type askpassFiles struct {
	dir        string
	scriptPath string
}

// newAskpassFiles writes a GIT_ASKPASS helper that answers git's username
// prompt with username (empty when the credential carries none) and the
// password prompt with the token — but only when the prompt names allowHost
// (the clone URL's bare hostname; ports match through the ":" alternative).
// Any other prompt (a redirect to another host, an unexpected question)
// exits nonzero, which fails the git command instead of leaking the
// credential.
func newAskpassFiles(username, password, allowHost string) (*askpassFiles, error) {
	dir, err := os.MkdirTemp("", "gotham-git-askpass-*")
	if err != nil {
		return nil, fmt.Errorf("deploy: git credential workspace: %w", err)
	}
	// The host is matched after "//" (no userinfo) or "@" (with userinfo),
	// and must be followed by "/", ":" or "'" — so a longer hostname sharing
	// the prefix (host.evil.com) or a path smuggling the hostname never
	// matches. Bracketed IPv6 literals match with their brackets (the prompt
	// carries them: https://[::1]:8443/…). The host stays double-quoted
	// (glob characters in it match literally) while the surrounding *
	// wildcards stay unquoted.
	host := allowHost
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	quoted := `"` + shellDoubleQuote(host)
	hostPattern := `*//` + quoted + `/"*` +
		`|*//` + quoted + `:"*` +
		`|*//` + quoted + `'"*` +
		`|*@` + quoted + `/"*` +
		`|*@` + quoted + `:"*` +
		`|*@` + quoted + `'"*`
	script := "#!/bin/sh\ncase \"$1\" in\n" + hostPattern + ") ;;\n*) exit 1 ;;\nesac\n" +
		"case \"$1\" in\n\"Username for\"*) printf '%s' " + shellQuote(username) +
		" ;;\n\"Password for\"*) printf '%s' " + shellQuote(password) + " ;;\n*) exit 1 ;;\nesac\n"
	path := dir + "/askpass.sh"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("deploy: write git credential helper: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("deploy: protect git credential helper: %w", err)
	}
	return &askpassFiles{dir: dir, scriptPath: path}, nil
}

// shellDoubleQuote embeds a value in a double-quoted shell word (a case
// pattern fragment here), escaping the characters that stay special inside
// double quotes. Glob characters need no escaping: quoted, they match
// literally — which is exactly what a hostname must do.
func shellDoubleQuote(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(value)
}

// remove deletes the ephemeral helper and its directory.
func (f *askpassFiles) remove() {
	_ = os.RemoveAll(f.dir)
}

// askpassEnv returns env plus the credential injection: GIT_ASKPASS pointing
// at the ephemeral helper, terminal prompts off, redirects refused (a remote
// that answers 301 fails the command instead of moving the token to another
// host), and the credential helpers disabled via the environment (so a
// control-plane gitconfig can neither override the helper nor prompt). The
// inherited GIT_ASKPASS and GIT_SSH_COMMAND are dropped first — an HTTPS
// clone must never spend an ambient SSH identity.
func (f *askpassFiles) askpassEnv(env []string) []string {
	env = withoutEnvPrefix(withoutEnv(withoutEnv(env, "GIT_SSH_COMMAND"), "GIT_ASKPASS"), "GIT_CONFIG_")
	return append(env,
		"GIT_ASKPASS="+f.scriptPath,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=http.followRedirects",
		"GIT_CONFIG_VALUE_1=false",
	)
}

// gitCredential resolves an application's HTTPS credential, turning a lookup
// failure into a clear error. Silently falling back to an anonymous request
// would hide the problem behind an auth failure from the Git host.
func (s gitSource) gitCredential(ctx context.Context, appID uuid.UUID) (username, token string, err error) {
	if s.creds == nil {
		return "", "", nil
	}
	username, token, err = s.creds.GitCredential(ctx, appID)
	if err != nil {
		return "", "", fmt.Errorf("deploy: load git credential: %w", err)
	}
	return strings.TrimSpace(username), strings.TrimSpace(token), nil
}

// cloneCredential is the credential selection for one clone or probe: the
// final URL (an https URL is rewritten to SSH only when a deploy key must
// carry it), the child environment carrying exactly one injection, the
// cleanup removing the ephemeral material, and which credential the log may
// name (never its value).
func (s gitSource) cloneCredential(ctx context.Context, app Application, rawURL string) (url string, env []string, cleanup func(), using string, err error) {
	cleanup = func() {}
	env = withoutEnv(withoutEnv(os.Environ(), "GIT_SSH_COMMAND"), "GIT_ASKPASS")
	if isHTTPSURL(rawURL) {
		username, token, credErr := s.gitCredential(ctx, app.ID)
		if credErr != nil {
			return "", nil, cleanup, "", credErr
		}
		if token != "" {
			// A cleartext http URL can never carry the token (see
			// isHTTPSURL): with no TLS the helper stays out and the URL
			// falls through to the keyed or anonymous path below.
			files, askErr := newAskpassFiles(username, token, privateGitHost(rawURL))
			if askErr != nil {
				return "", nil, cleanup, "", askErr
			}
			return rawURL, files.askpassEnv(env), files.remove, "https-credential", nil
		}
	}
	privatePEM, keyErr := s.deployKeyPEM(ctx, app.ID)
	if keyErr != nil {
		return "", nil, cleanup, "", keyErr
	}
	if privatePEM == "" {
		if isSSHTransportURL(rawURL) {
			// A keyless clone runs ssh with the control plane's ambient
			// identity: refuse, so an SSH URL on a credential-less source is
			// a clear validation error instead of an authentication surprise.
			return "", nil, cleanup, "", fmt.Errorf("%w: SSH clone URL %q needs a deploy key: generate one for this application, or use an https URL with a stored token",
				ErrValidation, RedactCloneURL(rawURL))
		}
		return rawURL, env, cleanup, "", nil
	}
	if isSSHTransportURL(rawURL) {
		files, filesErr := newDeployKeyFiles(privatePEM, s.log())
		if filesErr != nil {
			return "", nil, cleanup, "", filesErr
		}
		return rawURL, files.sshEnv(env), files.remove, "deploy-key", nil
	}
	// An https URL with a deploy key but no token keeps the historic
	// behaviour: the key is an SSH credential, so the URL is rewritten to
	// its SSH shape first (see sshCloneURL).
	url = sshCloneURL(rawURL)
	files, filesErr := newDeployKeyFiles(privatePEM, s.log())
	if filesErr != nil {
		return "", nil, cleanup, "", filesErr
	}
	return url, files.sshEnv(env), files.remove, "deploy-key", nil
}

// SetGitCredential stores (or rotates) an application's HTTPS token for
// git_private sources. The token is sealed with AES-256-GCM and never
// returned: the answer carries only whether a credential is now set and the
// username it was stored with.
func (s *Service) SetGitCredential(ctx context.Context, userID, appID uuid.UUID, username, token string) (GitCredentialState, error) {
	if !Enabled() {
		return GitCredentialState{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return GitCredentialState{}, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return GitCredentialState{}, err
	}
	if NormalizeSourceType(app.SourceType, app.Provider) != SourceGitPrivate {
		return GitCredentialState{}, fmt.Errorf("%w: HTTPS credentials are only stored for %q sources", ErrValidation, SourceGitPrivate)
	}
	username = strings.TrimSpace(username)
	token = strings.TrimSpace(token)
	if token == "" {
		return GitCredentialState{}, fmt.Errorf("%w: token is required", ErrValidation)
	}
	if isPlainHTTPURL(app.CloneURL) {
		return GitCredentialState{}, fmt.Errorf("%w: token authentication needs an https clone URL", ErrValidation)
	}
	if len(token) > maxGitTokenLength {
		return GitCredentialState{}, fmt.Errorf("%w: token is too long", ErrValidation)
	}
	if len(username) > 255 {
		return GitCredentialState{}, fmt.Errorf("%w: username is too long", ErrValidation)
	}
	sealed, err := providers.SealSecret(s.secret, token)
	if err != nil {
		return GitCredentialState{}, fmt.Errorf("deploy: seal git credential: %w", err)
	}
	if err := s.repo.UpsertGitCredential(ctx, app.ID, username, sealed); err != nil {
		return GitCredentialState{}, err
	}
	return GitCredentialState{HasCredential: true, Username: username}, nil
}

// GetGitCredential reports whether an application's HTTPS credential is set
// and the username it carries. The token itself is never returned.
func (s *Service) GetGitCredential(ctx context.Context, userID, appID uuid.UUID) (GitCredentialState, error) {
	if !Enabled() {
		return GitCredentialState{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return GitCredentialState{}, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return GitCredentialState{}, err
	}
	cred, err := s.repo.GetGitCredential(ctx, app.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return GitCredentialState{}, nil
		}
		return GitCredentialState{}, err
	}
	return GitCredentialState{HasCredential: true, Username: cred.Username}, nil
}

// DeleteGitCredential removes an application's HTTPS credential (switching
// back to a deploy key or an anonymous clone). It is idempotent: an
// application without one reports false and no error. The token itself is
// never returned on any path.
func (s *Service) DeleteGitCredential(ctx context.Context, userID, appID uuid.UUID) (bool, error) {
	if !Enabled() {
		return false, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return false, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return false, err
	}
	if err := s.repo.DeleteGitCredential(ctx, app.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetDeployKey returns an application's deploy key for the detail page: the
// public half, its fingerprint and the provider key ID. The private half is
// sealed on the server and never appears here.
func (s *Service) GetDeployKey(ctx context.Context, userID, appID uuid.UUID) (DeployKey, error) {
	if !Enabled() {
		return DeployKey{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return DeployKey{}, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return DeployKey{}, err
	}
	return s.repo.GetDeployKey(ctx, app.ID)
}

// gitProbeCooldown throttles the connection probe per user and application:
// the probe runs git against an operator-supplied URL on the request path,
// so a burst must not turn the control plane into a port-scan oracle. The
// throttle is per process (a note, not a distributed limit): a throttled
// call answers a failure outcome, never an error status.
const gitProbeCooldown = 10 * time.Second

// probeThrottleKey scopes one probe-throttle entry to the caller and the app.
type probeThrottleKey struct {
	userID uuid.UUID
	appID  uuid.UUID
}

// probeAllowed records a probe and reports whether one may run now.
func (s *Service) probeAllowed(userID, appID uuid.UUID) bool {
	key := probeThrottleKey{userID: userID, appID: appID}
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if s.probeLast == nil {
		s.probeLast = make(map[probeThrottleKey]time.Time)
	}
	if last, ok := s.probeLast[key]; ok && time.Since(last) < gitProbeCooldown {
		return false
	}
	s.probeLast[key] = time.Now()
	return true
}

// TestGitConnection probes an application's remote with git ls-remote and
// the stored credential, and classifies the outcome for the wizard/detail
// Test button. The probe mirrors the clone's credential selection, its
// message never carries secrets (the URL is redacted, the error tail is
// scrubbed), and Host names the remote for host-key pinning.
func (s *Service) TestGitConnection(ctx context.Context, userID, appID uuid.UUID) (GitConnectionResult, error) {
	if !Enabled() {
		return GitConnectionResult{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return GitConnectionResult{}, errors.New("deploy: repository is not configured")
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return GitConnectionResult{}, err
	}
	if NormalizeSourceType(app.SourceType, app.Provider) != SourceGitPrivate {
		return GitConnectionResult{}, fmt.Errorf("%w: connection testing is only available for %q sources", ErrValidation, SourceGitPrivate)
	}
	rawURL := strings.TrimSpace(app.CloneURL)
	if err := ValidatePrivateGitURL(rawURL); err != nil {
		return GitConnectionResult{}, err
	}
	// The probe runs git against an operator-supplied URL: throttle it per
	// caller and application so it cannot serve as a rapid port-scan oracle.
	if !s.probeAllowed(userID, app.ID) {
		return GitConnectionResult{
			Host:    privateGitHost(rawURL),
			Message: "a connection test ran recently; wait a moment and retry",
		}, nil
	}
	src := gitSource{keys: s.repo, creds: s.repo, logger: s.logger, run: s.testRunner()}
	url, env, cleanup, _, err := src.cloneCredential(ctx, app, rawURL)
	if err != nil {
		// A missing deploy key is a test outcome, not a request failure:
		// the wizard shows the message next to the Generate-key step.
		if errors.Is(err, ErrValidation) {
			return GitConnectionResult{Host: privateGitHost(rawURL), Message: err.Error()}, nil
		}
		return GitConnectionResult{}, err
	}
	defer cleanup()
	probeCtx, cancel := context.WithTimeout(ctx, gitTestTimeout)
	defer cancel()
	argv := []string{"git", "ls-remote", "--", url, "HEAD"}
	output, err := src.runOrDefault()(probeCtx, argv, env)
	if probeCtx.Err() != nil {
		return GitConnectionResult{
			Host:    privateGitHost(rawURL),
			Message: fmt.Sprintf("git ls-remote of %s timed out: check the URL and the network", RedactCloneURL(rawURL)),
		}, nil
	}
	if err != nil {
		msg := fmt.Sprintf("git ls-remote of %s failed", RedactCloneURL(rawURL))
		if quoted := tail(redactCloneError(string(output)), 400); quoted != "" {
			msg += ": " + quoted
		}
		msg += classifyGitFailure(string(output))
		if host := privateGitHost(rawURL); host != "" && isHostKeyFailure(string(output)) {
			msg += fmt.Sprintf(" (unknown host key for %s: pin it in %s — see the deploy-key panel)", host, knownHostsEnv)
		}
		return GitConnectionResult{Host: privateGitHost(rawURL), Message: msg}, nil
	}
	return GitConnectionResult{OK: true, Host: privateGitHost(rawURL), Message: "connection succeeded"}, nil
}

// testRunner returns the Service's git runner for the connection probe: the
// orchestrator source's fake in tests, nil (the real binary) in production.
func (s *Service) testRunner() cloneRunner {
	if s == nil || s.Orchestrator == nil {
		return nil
	}
	if src, ok := s.source.(gitSource); ok {
		return src.run
	}
	return nil
}

// isHostKeyFailure reports the ssh host-key verification failure, whose fix
// is pinning (not credentials): it gets its own hint appended after the
// generic classification.
func isHostKeyFailure(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "host key verification failed") ||
		strings.Contains(lower, "unknown host key") ||
		strings.Contains(lower, "no matching host key")
}

// runOrDefault executes git with the injected runner, or the real binary.
func (s gitSource) runOrDefault() cloneRunner {
	if s.run != nil {
		return s.run
	}
	return runGit
}
