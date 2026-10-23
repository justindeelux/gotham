package deploy

import (
	"context"
	"fmt"
	neturl "net/url"
	"strings"
	"unicode"
)

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
		if err := checkGitHostLiteral(parsed.Hostname()); err != nil {
			return err
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

var errPublicSSH = fmt.Errorf("%w: SSH clone URLs need a deploy key: use a public http(s) or git URL, or a private-git source",
	ErrValidation)

func hasURLWhitespace(url string) bool {
	for _, r := range url {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func isSSHTransportURL(url string) bool {
	if strings.Contains(url, "://") {
		scheme, _, _ := strings.Cut(url, "://")
		return strings.EqualFold(scheme, "ssh")
	}
	if strings.HasPrefix(url, "git@") {
		return true
	}
	if at := strings.Index(url, "@"); at > 0 {
		colon := strings.Index(url, ":")
		slash := strings.Index(url, "/")
		if colon > at && (slash == -1 || slash > at) {
			return true
		}
	}
	return false
}

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
		if quoted := quoteGitOutput(string(output)); quoted != "" {
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
