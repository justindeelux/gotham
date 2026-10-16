package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/toolcatalog"
)

// HostNetwork is the live host truth for the default-route interface, as
// reported by `gotham-hostctl status` (addresses/gateways from `ip`, modes
// from the winning .network file, effective DNS from resolvectl).
type HostNetwork struct {
	Interface  string     `json:"interface"`
	DNSServers []string   `json:"dns_servers"`
	IPv4       IPConfig   `json:"ipv4"`
	IPv6       IPv6Config `json:"ipv6"`
}

// HostApplier applies network and system settings to the host.
type HostApplier interface {
	// Probe reports what the host can apply and why it cannot; it never
	// fails, so a missing or unrunnable helper reports its cause instead of
	// an error.
	Probe(ctx context.Context) Host
	// InstallHostHelper installs the fixed host helper and its verb-pinned
	// sudoers rule through the root-owned tool installer, returning the
	// command output. It is the in-product action offered when the helper is
	// missing or not permitted.
	InstallHostHelper(ctx context.Context) (string, error)
	// HostNetwork reports the live addresses of the default-route
	// interface; ok is false when the helper is missing or the probe fails.
	HostNetwork(ctx context.Context) (live HostNetwork, ok bool)
	// ApplyNetwork applies net tentatively; the host reverts it by itself
	// unless ConfirmNetwork runs within revertAfter. dnsOnly writes only
	// the resolver drop-in and never touches the interface file, so a DNS
	// change on a static host cannot drop its address.
	ApplyNetwork(ctx context.Context, net Network, revertAfter time.Duration, dnsOnly bool) error
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

// ToolctlEnv overrides the root-owned tool installer path (tests).
const ToolctlEnv = "GOTHAM_TOOLCTL"

// DefaultToolctl is the fixed, root-owned tool installer (deploy/gotham-toolctl.sh).
const DefaultToolctl = "/usr/libexec/gotham/gotham-toolctl"

// DefaultBootstrapPayload is the root-owned payload the installer and the
// self-update wrapper install (deploy/gotham-hostbootstrap.sh). Its presence is
// what makes the in-product helper install possible.
const DefaultBootstrapPayload = "/usr/libexec/gotham/hostbootstrap"

// hostctlToolID is the tool-installer id that maps to the host-helper install
// (see the allow-list in deploy/gotham-toolctl.sh).
const hostctlToolID = "gotham-hostctl"

// sudoApplier runs the helper through `sudo -n` with a closed verb set; the
// values travel on stdin as key=value lines and are re-validated by the
// helper. The path is never taken from request data.
type sudoApplier struct {
	path    string
	toolctl string
}

// NewSudoApplier returns the production applier. Without the helper file it
// reports no capabilities, so the host sections stay read-only.
func NewSudoApplier() HostApplier {
	return &sudoApplier{
		path:    envOr(HelperEnv, DefaultHelper),
		toolctl: envOr(ToolctlEnv, DefaultToolctl),
	}
}

// envOr returns the environment override or the default.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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

// Probe reports the host's readiness without ever failing.
func (a *sudoApplier) Probe(ctx context.Context) Host {
	h := Host{
		Helper:       HelperReady,
		Bootstrap:    payloadPresent(DefaultBootstrapPayload),
		NetworkStack: networkStackAvailable(),
		SystemStack:  systemStackAvailable(),
	}
	if _, err := os.Stat(a.path); err != nil {
		h.Helper = HelperMissing
		return h
	}
	p, ok := a.fetchStatus(ctx)
	if !ok {
		// The file is there but the helper did not answer: sudo -n refused it
		// because its verb-pinned sudoers rule is missing.
		h.Helper = HelperDenied
		return h
	}
	h.Network = p.Network
	h.System = p.System
	return h
}

// payloadPresent reports whether the root-owned host-helper payload is
// installed, i.e. whether the in-product install has a privileged path to run.
func payloadPresent(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "gotham-hostctl"))
	return err == nil && fi.Mode().IsRegular()
}

// InstallHostHelper runs the fixed tool installer to place the host helper and
// its sudoers rule. The tool installer owns the content and re-validates the
// id, so the control plane only names the catalog id.
func (a *sudoApplier) InstallHostHelper(ctx context.Context) (string, error) {
	if _, err := os.Stat(a.toolctl); err != nil {
		return "", fmt.Errorf("%w: the host tool installer is not present on this host", ErrHost)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", a.toolctl, "install-tool", hostctlToolID) //nolint:gosec // fixed path and id
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%w: install host helper: %s", ErrHost, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// networkStackAvailable reports whether the host runs the stack the network
// section needs, probed without the helper so a missing helper does not hide
// an unsupported host.
func networkStackAvailable() bool {
	return toolcatalog.Installed(toolcatalog.IDSystemdNetworkd) &&
		toolcatalog.Installed(toolcatalog.IDSystemdResolved) &&
		toolcatalog.Installed(toolcatalog.IDNetworkctl)
}

// systemStackAvailable reports whether the host has what the system section
// needs (systemctl and hostnamectl).
func systemStackAvailable() bool {
	if !toolcatalog.Installed(toolcatalog.IDSystemctl) {
		return false
	}
	_, err := exec.LookPath("hostnamectl")
	return err == nil
}

// statusPayload mirrors the helper's status JSON; host_network is null when
// the default-route interface cannot be probed.
type statusPayload struct {
	Capabilities
	HostNetwork *HostNetwork `json:"host_network"`
}

func (a *sudoApplier) fetchStatus(ctx context.Context) (*statusPayload, bool) {
	out, err := a.run(ctx, "status", "")
	if err != nil {
		return nil, false
	}
	var p statusPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &p) != nil {
		return nil, false
	}
	return &p, true
}

// HostNetwork reports the live host configuration (see fetchStatus).
func (a *sudoApplier) HostNetwork(ctx context.Context) (HostNetwork, bool) {
	if _, err := os.Stat(a.path); err != nil {
		return HostNetwork{}, false
	}
	p, ok := a.fetchStatus(ctx)
	if !ok || p.HostNetwork == nil || p.HostNetwork.Interface == "" {
		return HostNetwork{}, false
	}
	return *p.HostNetwork, true
}

func (a *sudoApplier) ApplyNetwork(ctx context.Context, n Network, revertAfter time.Duration, dnsOnly bool) error {
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
	line("dns_only", strconv.FormatBool(dnsOnly))
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
