package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// HostApplier applies network and system settings to the host.
type HostApplier interface {
	// Capabilities reports what the host can apply; it never fails (an
	// unusable helper reports no capability).
	Capabilities(ctx context.Context) Capabilities
	// ApplyNetwork applies net tentatively; the host reverts it by itself
	// unless ConfirmNetwork runs within revertAfter.
	ApplyNetwork(ctx context.Context, net Network, revertAfter time.Duration) error
	// ConfirmNetwork keeps the applied network change.
	ConfirmNetwork(ctx context.Context) error
	// RevertNetwork restores the pre-change network configuration now.
	RevertNetwork(ctx context.Context) error
	// ApplySystem applies hostname and NTP settings.
	ApplySystem(ctx context.Context, sys System) error
}

// HelperEnv overrides the helper path (tests, non-standard layouts).
const HelperEnv = "GOTHAM_HOSTCTL"

// DefaultHelper is the fixed, root-owned host helper.
const DefaultHelper = "/usr/libexec/gotham/gotham-hostctl"

// sudoApplier runs the helper through `sudo -n` with a closed verb set; the
// values travel on stdin as key=value lines and are re-validated by the
// helper. The path is never taken from request data.
type sudoApplier struct{ path string }

// NewSudoApplier returns the production applier. Without the helper file it
// reports no capabilities, so the host sections stay read-only.
func NewSudoApplier() HostApplier {
	path := os.Getenv(HelperEnv)
	if path == "" {
		path = DefaultHelper
	}
	return &sudoApplier{path: path}
}

func (a *sudoApplier) run(ctx context.Context, verb, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// verb is one of a fixed set of constants below, never request data.
	cmd := exec.CommandContext(ctx, "sudo", "-n", a.path, verb) //nolint:gosec // fixed path and verbs
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s: %s", ErrHost, verb, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

func (a *sudoApplier) Capabilities(ctx context.Context) Capabilities {
	if _, err := os.Stat(a.path); err != nil {
		return Capabilities{}
	}
	out, err := a.run(ctx, "status", "")
	if err != nil {
		return Capabilities{}
	}
	var caps Capabilities
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &caps) != nil {
		return Capabilities{}
	}
	return caps
}

func (a *sudoApplier) ApplyNetwork(ctx context.Context, n Network, revertAfter time.Duration) error {
	var b strings.Builder
	line := func(k, v string) { b.WriteString(k + "=" + v + "\n") }
	line("dns", strings.Join(n.DNSServers, ","))
	line("ipv4_mode", n.IPv4.Mode)
	line("ipv4_address", n.IPv4.Address)
	line("ipv4_gateway", n.IPv4.Gateway)
	line("ipv6_enabled", strconv.FormatBool(n.IPv6.Enabled))
	line("ipv6_mode", n.IPv6.Mode)
	line("ipv6_address", n.IPv6.Address)
	line("ipv6_gateway", n.IPv6.Gateway)
	line("revert_after", strconv.Itoa(int(revertAfter.Seconds())))
	_, err := a.run(ctx, "apply-network", b.String())
	return err
}

func (a *sudoApplier) ConfirmNetwork(ctx context.Context) error {
	_, err := a.run(ctx, "confirm-network", "")
	return err
}

func (a *sudoApplier) RevertNetwork(ctx context.Context) error {
	_, err := a.run(ctx, "revert-network", "")
	return err
}

func (a *sudoApplier) ApplySystem(ctx context.Context, s System) error {
	in := "hostname=" + s.Hostname + "\n" +
		"ntp_enabled=" + strconv.FormatBool(s.NTPEnabled) + "\n" +
		"ntp_servers=" + strings.Join(s.NTPServers, ",") + "\n"
	_, err := a.run(ctx, "apply-system", in)
	return err
}
