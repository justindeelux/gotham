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
			if err := rejectLeadingDash("host", host); err != nil {
				return err
			}
			return checkGitHostLiteral(host)
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
		return checkGitHostLiteral(parsed.Hostname())
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
		return checkGitHostLiteral(parsed.Hostname())
	case "file":
		if devLocalClone() {
			return nil
		}
		return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
	default:
		return fmt.Errorf("%w: unsupported clone URL scheme %q: use ssh, scp-like or https", ErrValidation, parsed.Scheme)
	}
}

func rejectLeadingDash(what, value string) error {
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%w: unsupported clone URL: %s must not start with '-'", ErrValidation, what)
	}
	return nil
}

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

func isHTTPSURL(raw string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return false
	}
	return strings.EqualFold(scheme, "https")
}

func isPlainHTTPURL(raw string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return false
	}
	return strings.EqualFold(scheme, "http")
}

func privateGitHost(raw string) string {
	url := strings.TrimSpace(raw)
	if strings.Contains(url, "://") {
		if parsed, err := neturl.Parse(url); err == nil {
			return parsed.Hostname()
		}
		return ""
	}
	_, host := splitScpAuthority(url)
	if host == "" || strings.Contains(host, "/") || !strings.Contains(url, ":") {
		return ""
	}
	return host
}

type gitCredentialResolver interface {
	GitCredential(ctx context.Context, appID uuid.UUID) (username, token string, err error)
}

type GitCredential struct {
	Username  string
	UpdatedAt time.Time
}

type GitCredentialState struct {
	HasCredential bool
	Username      string
}

type GitConnectionResult struct {
	OK      bool
	Message string
	Host    string
}

const gitTestTimeout = 30 * time.Second

const maxGitTokenLength = 8192

type askpassFiles struct {
	dir        string
	scriptPath string
}

func newAskpassFiles(username, password, allowHost string) (*askpassFiles, error) {
	dir, err := os.MkdirTemp("", "gotham-git-askpass-*")
	if err != nil {
		return nil, fmt.Errorf("deploy: git credential workspace: %w", err)
	}
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

func shellDoubleQuote(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(value)
}

func (f *askpassFiles) remove() {
	_ = os.RemoveAll(f.dir)
}

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

func (s gitSource) cloneCredential(ctx context.Context, app Application, rawURL string) (url string, env []string, cleanup func(), using string, err error) {
	cleanup = func() {}
	env = withoutEnv(withoutEnv(os.Environ(), "GIT_SSH_COMMAND"), "GIT_ASKPASS")
	if isHTTPSURL(rawURL) {
		username, token, credErr := s.gitCredential(ctx, app.ID)
		if credErr != nil {
			return "", nil, cleanup, "", credErr
		}
		if token != "" {
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
	url = sshCloneURL(rawURL)
	files, filesErr := newDeployKeyFiles(privatePEM, s.log())
	if filesErr != nil {
		return "", nil, cleanup, "", filesErr
	}
	return url, files.sshEnv(env), files.remove, "deploy-key", nil
}

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

const gitProbeCooldown = 10 * time.Second

type probeThrottleKey struct {
	userID uuid.UUID
	appID  uuid.UUID
}

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
	if !s.probeAllowed(userID, app.ID) {
		return GitConnectionResult{
			Host:    privateGitHost(rawURL),
			Message: "a connection test ran recently; wait a moment and retry",
		}, nil
	}
	src := gitSource{keys: s.repo, creds: s.repo, logger: s.logger, run: s.testRunner(), lookup: s.probeLookup()}
	url, env, cleanup, _, err := src.cloneCredential(ctx, app, rawURL)
	if err != nil {
		if errors.Is(err, ErrValidation) {
			return GitConnectionResult{Host: privateGitHost(rawURL), Message: err.Error()}, nil
		}
		return GitConnectionResult{}, err
	}
	defer cleanup()
	probeCtx, cancel := context.WithTimeout(ctx, gitTestTimeout)
	defer cancel()
	pinnedEnv, err := pinGitRemoteHost(probeCtx, s.probeLookup(), url, env)
	if err != nil {
		return GitConnectionResult{Host: privateGitHost(rawURL), Message: err.Error()}, nil
	}
	env = pinnedEnv
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
		if quoted := quoteGitOutput(string(output)); quoted != "" {
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

func (s *Service) probeLookup() gitHostLookupFunc {
	if s == nil || s.Orchestrator == nil {
		return nil
	}
	return s.gitLookup
}

func (s *Service) testRunner() cloneRunner {
	if s == nil || s.Orchestrator == nil {
		return nil
	}
	if src, ok := s.source.(gitSource); ok {
		return src.run
	}
	return nil
}

func isHostKeyFailure(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "host key verification failed") ||
		strings.Contains(lower, "unknown host key") ||
		strings.Contains(lower, "no matching host key")
}

func (s gitSource) runOrDefault() cloneRunner {
	if s.run != nil {
		return s.run
	}
	return runGit
}
