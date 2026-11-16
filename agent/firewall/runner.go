package firewall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// HelperPath is the fixed root-owned helper installed by the agent installer.
// The agent reaches exactly its firewall verbs through the sudoers drop-in.
const HelperPath = "/usr/libexec/gotham/gotham-hostctl"

const helperTimeout = 60 * time.Second

// installTimeout covers a package manager run, which outlasts a rule edit.
const installTimeout = 4 * time.Minute

// Runner runs one host-helper verb with its key=value input.
type Runner interface {
	Run(ctx context.Context, verb, stdin string) (string, error)
}

type helperError struct {
	verb   string
	code   int
	reason string
	err    error
}

func (e *helperError) Error() string {
	if e.reason != "" {
		return e.reason
	}
	return fmt.Sprintf("gotham-hostctl %s failed", e.verb)
}

func (e *helperError) Unwrap() error { return e.err }

type sudoRunner struct {
	helper string
}

func (r sudoRunner) Run(ctx context.Context, verb, stdin string) (string, error) {
	timeout := helperTimeout
	if verb == "firewall-install" {
		timeout = installTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", r.helper, verb) //nolint:gosec // fixed helper and verb
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return "", &helperError{verb: verb, code: code, reason: helperReason(errOut.String()), err: err}
	}
	return out.String(), nil
}

// helperReason returns the helper's own last diagnostic line, bounded. Raw
// command stderr may name paths and is never shown.
func helperReason(stderr string) string {
	var reason string
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "gotham-hostctl: ") {
			reason = line
		}
	}
	const maxReason = 300
	if len(reason) > maxReason {
		reason = reason[:maxReason]
	}
	return reason
}
