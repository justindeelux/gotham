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

type Source interface {
	Clone(ctx context.Context, app Application, dir string, log func(string)) error
}

type deployKeyResolver interface {
	DeployKeyPrivatePEM(ctx context.Context, appID uuid.UUID) (string, error)
}

type appTokenResolver interface {
	TokenCloneURL(ctx context.Context, userID, appID uuid.UUID, repo, cloneURL string) (string, error)
}

type cloneRunner func(ctx context.Context, argv []string, env []string) ([]byte, error)

type gitSource struct {
	keys      deployKeyResolver
	appTokens appTokenResolver
	creds     gitCredentialResolver
	run       cloneRunner
	lookup    gitHostLookupFunc
	logger    *slog.Logger
}

var _ Source = gitSource{}

const defaultBranch = "main"

func (s gitSource) Clone(ctx context.Context, app Application, dir string, log func(string)) error {
	url := strings.TrimSpace(app.CloneURL)
	if err := validateCloneURL(url); err != nil {
		return err
	}
	tokenClone := false
	if app.GitHubAppID != uuid.Nil && s.appTokens != nil && isHTTPCloneURL(url) {
		tokenURL, err := s.appTokens.TokenCloneURL(ctx, app.UserID, app.GitHubAppID, app.Repo, url)
		if err != nil {
			return err
		}
		url = tokenURL
		tokenClone = true
	} else if app.GitHubAppID == uuid.Nil && app.SourceType == SourceGitHubApp && isHTTPCloneURL(url) && log != nil {
		log("no GitHub App connection is linked; cloning without an installation token")
	}
	var (
		env     []string
		cleanup func()
		using   string
	)
	if tokenClone {
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
	pinnedURL, pinnedEnv, err := pinGitRemoteHost(ctx, s.lookup, url, env)
	if err != nil {
		return err
	}
	env = pinnedEnv
	if using == "deploy-key" && devAcceptNewHostKeys() {
		s.log().Warn("deploy: keyed clone trusts unseen host keys",
			"reason", devAcceptNewHostKeysEnv+" is set",
			"risk", "the clone accepts any host key; disable this in production",
		)
	}
	branch := strings.TrimSpace(app.Branch)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("git clone: clear %s: %w", dir, err)
	}

	runner := s.runOrDefault()
	if branch == "" {
		resolved, err := defaultBranchFor(ctx, runner, pinnedURL, env)
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
		"--depth", "1", "--single-branch", "--branch", branch, "--", pinnedURL, dir}
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

func (s gitSource) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

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

const gitWaitDelay = 10 * time.Second

func runGit(ctx context.Context, argv []string, env []string) ([]byte, error) {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Env = env
	cleanup := configureGitCommand(command)
	defer cleanup()
	return command.CombinedOutput()
}

type deployKeyFiles struct {
	dir            string
	keyPath        string
	knownHostsPath string
}

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

func (f *deployKeyFiles) remove() {
	_ = os.RemoveAll(f.dir)
}

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

func (f *deployKeyFiles) sshEnv(env []string) []string {
	command := "ssh -F /dev/null -i " + shellQuote(f.keyPath) +
		" -o IdentitiesOnly=yes" +
		" -o UserKnownHostsFile=" + shellQuote(f.knownHostsPath) +
		" -o GlobalKnownHostsFile=/dev/null" +
		" -o StrictHostKeyChecking=" + strictHostKeyChecking(devAcceptNewHostKeys())
	return append(env, "GIT_SSH_COMMAND="+command)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

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

const devLocalCloneEnv = "GOTHAM_DEV_CLONE_LOCAL"

func devLocalClone() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(devLocalCloneEnv)), "true")
}

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
		parsed, err := neturl.Parse(url)
		if err != nil {
			return fmt.Errorf("%w: unsupported clone URL", ErrValidation)
		}
		if err := rejectLeadingDash("host", parsed.Hostname()); err != nil {
			return err
		}
		if user := parsed.User.Username(); user != "" {
			if err := rejectLeadingDash("user", user); err != nil {
				return err
			}
		}
		if parsed.Hostname() != "" && !strings.EqualFold(parsed.Scheme, "file") {
			if err := checkGitHostLiteral(parsed.Hostname()); err != nil {
				return err
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

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func RedactCloneURL(raw string) string {
	parsed, err := neturl.Parse(raw)
	if err != nil {
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

func redactCloneError(msg string) string {
	return userinfoPattern.ReplaceAllString(msg, "$1***@")
}

var userinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s]+@`)
