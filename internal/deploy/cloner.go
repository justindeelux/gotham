package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Source prepares the build tree for a deployment: it checks the
// application's repository out into dir. Production shells out to git;
// tests substitute a fake that writes a fixture tree.
type Source interface {
	Clone(ctx context.Context, app Application, dir string, log func(string)) error
}

// gitSource clones over HTTPS/SSH with the git binary on the control plane.
// The result is a shallow, single-branch working tree that the build engines
// consume directly (they exclude .git from the context themselves).
type gitSource struct{}

// Compile-time guarantee that gitSource satisfies the Source seam.
var _ Source = gitSource{}

// defaultBranch is used when an application carries no branch (older rows or
// a provider that reported an empty default).
const defaultBranch = "main"

// Clone implements Source. The clone URL is passed after "--" so a hostile
// value can never be parsed as an option, and only the known git URL shapes
// are accepted.
func (gitSource) Clone(ctx context.Context, app Application, dir string, log func(string)) error {
	url := strings.TrimSpace(app.CloneURL)
	if err := validateCloneURL(url); err != nil {
		return err
	}
	branch := strings.TrimSpace(app.Branch)
	if branch == "" {
		branch = defaultBranch
	}
	// A retried step re-enters Clone with the same directory: clear whatever
	// the previous attempt left behind so git does not refuse a non-empty target.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("git clone: clear %s: %w", dir, err)
	}

	if log != nil {
		log(fmt.Sprintf("git clone --depth 1 --branch %s %s", branch, url))
	}
	command := exec.CommandContext(ctx, "git", "clone",
		"--depth", "1", "--single-branch", "--branch", branch, "--", url, dir)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("git clone: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("git clone: %w: %s", err, tail(string(output), 400))
	}
	if log != nil {
		log("repository cloned (" + branch + ")")
	}
	return nil
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
