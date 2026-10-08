package deploy

import (
	"context"
	"fmt"
	neturl "net/url"
	"strings"
	"unicode"
)

// Public git sources (GS-3) are keyless by definition: the control plane
// clones them with no deploy key and no provider credentials. The validation
// below keeps that property structural — only credential-free http(s)/git
// URLs are accepted, so an SSH URL can never reach a clone that would spend
// the control plane's ambient ~/.ssh identity, and a token-bearing URL can
// never be stored as clone_url.

// ValidatePublicGitURL accepts the clone URLs a keyless public-git source
// may carry: http, https or git, with a host and no userinfo. SSH transports
// (ssh:// and scp-like git@host:path) are refused with a message naming the
// alternative, because they need a credential the public flow never has.
// Local paths and file:// URLs exist only for development fixtures behind
// GOTHAM_DEV_CLONE_LOCAL; production keeps the allow-list remote-only.
func ValidatePublicGitURL(raw string) error {
	url := strings.TrimSpace(raw)
	if url == "" {
		return fmt.Errorf("%w: application has no clone URL", ErrValidation)
	}
	if hasURLWhitespace(url) {
		return fmt.Errorf("%w: unsupported clone URL: must not contain whitespace", ErrValidation)
	}
	if !strings.Contains(url, "://") {
		if isSSHTransportURL(url) {
			return errPublicSSH
		}
		if strings.HasPrefix(url, "/") {
			if devLocalClone() {
				return nil
			}
			return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
		}
		return fmt.Errorf("%w: unsupported clone URL: use http(s) or git", ErrValidation)
	}
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("%w: unsupported clone URL", ErrValidation)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "git":
		if parsed.Hostname() == "" {
			return fmt.Errorf("%w: clone URL has no host", ErrValidation)
		}
		if parsed.User != nil {
			return fmt.Errorf("%w: clone URL must not embed credentials: use a clean public URL", ErrValidation)
		}
		return nil
	case "ssh":
		return errPublicSSH
	case "file":
		if devLocalClone() {
			return nil
		}
		return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
	default:
		return fmt.Errorf("%w: unsupported clone URL scheme %q: use http(s) or git", ErrValidation, parsed.Scheme)
	}
}

// errPublicSSH is the shared refusal for SSH transports on a keyless source.
var errPublicSSH = fmt.Errorf("%w: SSH clone URLs need a deploy key: use a public http(s) or git URL, or a private-git source",
	ErrValidation)

// hasURLWhitespace reports whitespace and control characters, which can
// smuggle a second argv token or a newline into logs.
func hasURLWhitespace(url string) bool {
	for _, r := range url {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// isSSHTransportURL reports the transports a keyless clone must never
// attempt: ssh:// URLs and scp-like [user@]host:path values. It is the
// deploy-time half of ValidatePublicGitURL: creation validation keeps new
// rows clean, this keeps legacy rows (stored before GS-3) from spending the
// control plane's ambient SSH identity.
func isSSHTransportURL(url string) bool {
	if strings.Contains(url, "://") {
		scheme, _, _ := strings.Cut(url, "://")
		return strings.EqualFold(scheme, "ssh")
	}
	if strings.HasPrefix(url, "git@") {
		return true
	}
	// Generic scp syntax [user@]host:path: an "@" before the first ":" and
	// no "/" before that "@" (a path never looks like that).
	if at := strings.Index(url, "@"); at > 0 {
		colon := strings.Index(url, ":")
		slash := strings.Index(url, "/")
		if colon > at && (slash == -1 || slash > at) {
			return true
		}
	}
	return false
}

// defaultBranchFor resolves the default branch of a remote with
// git ls-remote --symref, so an application with no branch pins whatever HEAD
// points at instead of guessing "main". Failures carry the classified hint
// (branch not found, unreachable, not found, needs credentials) the clone
// path reuses.
func defaultBranchFor(ctx context.Context, run cloneRunner, rawURL string, env []string) (string, error) {
	runner := run
	if runner == nil {
		runner = runGit
	}
	argv := []string{"git", "ls-remote", "--symref", "--", rawURL, "HEAD"}
	output, err := runner(ctx, argv, env)
	if ctx.Err() != nil {
		return "", fmt.Errorf("git ls-remote: %w", ctx.Err())
	}
	if err != nil {
		msg := fmt.Sprintf("git ls-remote: resolve the default branch of %s",
			RedactCloneURL(rawURL))
		if quoted := tail(redactCloneError(string(output)), 400); quoted != "" {
			msg += ": " + quoted
		}
		return "", fmt.Errorf("%s: %w%s", msg, err, classifyGitFailure(string(output)))
	}
	if branch := parseSymrefBranch(string(output)); branch != "" {
		return branch, nil
	}
	return "", fmt.Errorf("%w: git ls-remote of %s reported no default branch",
		ErrValidation, RedactCloneURL(rawURL))
}

// parseSymrefBranch reads the `ref: refs/heads/<name>\tHEAD` line ls-remote
// --symref prints, and answers "" when no well-formed default is present.
func parseSymrefBranch(output string) string {
	for line := range strings.Lines(strings.TrimSpace(output)) {
		ref, _, _ := strings.Cut(line, "\t")
		name, ok := strings.CutPrefix(strings.TrimSpace(ref), "ref: refs/heads/")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, " \t\r\n~^:?*[\\") || strings.HasPrefix(name, "-") {
			continue
		}
		return name
	}
	return ""
}

// classifyGitFailure maps git stderr to the actionable hint the wizard and
// the deploy log append after the quoted error. A repository that answers
// "not found" over https may simply be private (hosts hide existence), so
// that hint names both cases; "" means no known shape matched. Status codes
// are matched with their git phrasing ("returned error: 403"), never bare
// digits, so a port number can never trigger the auth hint.
func classifyGitFailure(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "remote branch") && strings.Contains(lower, "not found"):
		return " (branch not found: check the branch name)"
	case strings.Contains(lower, "repository not found") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "could not find remote") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "no such repository"):
		return " (repository not found: check the URL, or the repository may be private)"
	case strings.Contains(lower, "could not read username") ||
		strings.Contains(lower, "terminal prompts disabled") ||
		strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "auth failed") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "could not read from remote repository") ||
		strings.Contains(lower, "invalid username") ||
		strings.Contains(lower, "askpass") ||
		strings.Contains(lower, "returned error: 401") ||
		strings.Contains(lower, "returned error: 403") ||
		strings.Contains(lower, "error: 401") ||
		strings.Contains(lower, "error: 403"):
		return " (authentication is required: the repository may be private, use a credentialed source)"
	case strings.Contains(lower, "could not resolve host") ||
		strings.Contains(lower, "unable to connect") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "connection timed out") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "temporary failure"):
		return " (the host is unreachable: check the URL and the network)"
	default:
		return ""
	}
}
