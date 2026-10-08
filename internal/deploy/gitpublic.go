package deploy

import (
	"context"
	"fmt"
	"strings"
)

// Public git sources (GS-3) are keyless by definition: the control plane
// clones them with no deploy key and no provider credentials. The validation
// below keeps that property structural — only schemes git can fetch
// anonymously are accepted, so an SSH URL can never reach a clone that would
// spend the control plane's ambient ~/.ssh identity on it.

// ValidatePublicGitURL accepts the clone URLs a keyless public-git source
// may carry: http, https or git. SSH transports (ssh:// and scp-like
// git@host:path) are refused with a message naming the alternative, because
// they need a credential the public flow never has. Local paths and file://
// URLs exist only for development fixtures behind GOTHAM_DEV_CLONE_LOCAL;
// production keeps the allow-list remote-only.
func ValidatePublicGitURL(raw string) error {
	url := strings.TrimSpace(raw)
	if url == "" {
		return fmt.Errorf("%w: application has no clone URL", ErrValidation)
	}
	if strings.Contains(url, "://") {
		scheme, _, _ := strings.Cut(url, "://")
		switch strings.ToLower(scheme) {
		case "http", "https", "git":
			return nil
		case "ssh":
			return fmt.Errorf("%w: SSH clone URLs need a deploy key: use a public http(s) or git URL, or a private-git source", ErrValidation)
		case "file":
			if devLocalClone() {
				return nil
			}
			return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
		default:
			return fmt.Errorf("%w: unsupported clone URL scheme %q: use http(s) or git", ErrValidation, scheme)
		}
	}
	if isScpLikeCloneURL(url) {
		return fmt.Errorf("%w: SSH clone URLs need a deploy key: use a public http(s) or git URL, or a private-git source", ErrValidation)
	}
	if strings.HasPrefix(url, "/") {
		if devLocalClone() {
			return nil
		}
		return fmt.Errorf("%w: local clone sources are disabled", ErrValidation)
	}
	return fmt.Errorf("%w: unsupported clone URL: use http(s) or git", ErrValidation)
}

// isScpLikeCloneURL reports the ssh/scp transports a keyless clone must
// never attempt: ssh:// URLs and scp-like [user@]host:path values. It is the
// deploy-time half of ValidatePublicGitURL: creation validation keeps new
// rows clean, this keeps legacy rows (stored before GS-3) from spending the
// control plane's ambient SSH identity.
func isScpLikeCloneURL(url string) bool {
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
// (unreachable, not found, needs credentials) the clone path reuses.
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
		detail := tail(redactCloneError(string(output)), 400)
		if detail != "" {
			detail = ": " + detail
		}
		return "", fmt.Errorf("git ls-remote: resolve the default branch of %s%s%s: %w",
			redactCloneURL(rawURL), classifyGitFailure(string(output)), detail, err)
	}
	if branch := parseSymrefBranch(string(output)); branch != "" {
		return branch, nil
	}
	return "", fmt.Errorf("%w: git ls-remote of %s reported no default branch",
		ErrValidation, redactCloneURL(rawURL))
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
// that hint names both cases; "" means no known shape matched.
func classifyGitFailure(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "repository not found") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "could not find remote") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "no such repository"):
		return " (repository not found: check the URL, or the repository may be private)"
	case strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "auth failed") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "could not read from remote repository") ||
		strings.Contains(lower, "invalid username") ||
		strings.Contains(lower, "askpass") ||
		strings.Contains(lower, "401") ||
		strings.Contains(lower, "403"):
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
