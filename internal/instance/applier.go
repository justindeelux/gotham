package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/toolcatalog"
)

// HostApplier reads and changes one node's host settings. The local node uses
// the control plane's sudo helper; every other node uses the agent's
// HostService. Both re-validate every value on the host, so this interface is
// transport, not a trust boundary.
type HostApplier interface {
	Status(ctx context.Context) (HostStatus, error)
	InstallHostHelper(ctx context.Context) (string, error)
	ApplyNetwork(ctx context.Context, net Network, revertAfter time.Duration, dnsOnly bool) error
	ConfirmNetwork(ctx context.Context) error
	RevertNetwork(ctx context.Context) error
	ApplySystem(ctx context.Context, sys System) error
	// CleanupStatus reports the root-only cleanup categories this host
	// supports; ErrHelperOutdated means the helper predates them.
	CleanupStatus(ctx context.Context) (HostCleanupStatus, error)
	// CleanupHost reclaims one category and returns the bytes it released.
	CleanupHost(ctx context.Context, category string, minAge time.Duration) (int64, error)
}

// HostCleanupCategory is one root-only cleanup category's state on a host.
type HostCleanupCategory struct {
	Supported bool
	Manager   string
	Bytes     int64
}

// HostCleanupStatus is the journal and package-cache state of one host.
type HostCleanupStatus struct {
	Journal      HostCleanupCategory
	PackageCache HostCleanupCategory
}

const HelperEnv = "GOTHAM_HOSTCTL"

const DefaultHelper = "/usr/libexec/gotham/gotham-hostctl"

const ToolctlEnv = "GOTHAM_TOOLCTL"

const DefaultToolctl = "/usr/libexec/gotham/gotham-toolctl"

const DefaultBootstrapPayload = "/usr/libexec/gotham/hostbootstrap"

const PayloadEnv = "GOTHAM_HOSTBOOTSTRAP_PAYLOAD"

const hostctlToolID = "gotham-hostctl"

const repairProbeTimeout = 5 * time.Second

type sudoApplier struct {
	path    string
	toolctl string
	payload string
}

func NewSudoApplier() HostApplier {
	return &sudoApplier{
		path:    envOr(HelperEnv, DefaultHelper),
		toolctl: envOr(ToolctlEnv, DefaultToolctl),
		payload: envOr(PayloadEnv, DefaultBootstrapPayload),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// helperExit keeps the helper's pinned exit code while still reading as
// ErrHost, so a caller can tell "unsupported" (3) from "unknown verb" (2).
type helperExit struct {
	code int
	err  error
}

func (e *helperExit) Error() string { return e.err.Error() }
func (e *helperExit) Unwrap() error { return e.err }

func (a *sudoApplier) run(ctx context.Context, verb, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", a.path, verb) //nolint:gosec // fixed path and verbs
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return "", &helperExit{
			code: code,
			err:  fmt.Errorf("%w: %s: %s", ErrHost, verb, strings.TrimSpace(errOut.String())),
		}
	}
	return out.String(), nil
}

// cleanupRunError maps the helper's cleanup exit codes to the sentinels the
// cleanup service routes on: 3 is an unsupported host, 2 is a helper that does
// not know the verb (it predates the cleanup contract).
func cleanupRunError(err error) error {
	var exit *helperExit
	if errors.As(err, &exit) {
		switch exit.code {
		case 2:
			return fmt.Errorf("%w: %v", ErrHelperOutdated, err)
		case 3:
			return fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
	}
	return err
}

func (a *sudoApplier) Status(ctx context.Context) (HostStatus, error) {
	if _, err := os.Stat(a.path); err != nil {
		return HostStatus{Capability: CapabilityHelperMissing, Repair: a.repairPossible(ctx)}, nil
	}
	p, ok := a.fetchStatus(ctx)
	if !ok {
		return HostStatus{Capability: CapabilityHelperDenied, Repair: a.repairPossible(ctx)}, nil
	}
	if p.Timezone == nil {
		return HostStatus{
			Capability: CapabilityHelperOutdated,
			Repair:     a.repairPossible(ctx),
		}, nil
	}
	if !p.Network || !p.System || !networkStackAvailable() || !systemStackAvailable() {
		return HostStatus{Capability: CapabilityUnsupportedStack}, nil
	}
	return HostStatus{
		Capability: CapabilityReady,
		Network:    networkFromHost(p.HostNetwork),
		System: System{
			Hostname: p.Hostname, NTPEnabled: p.NTPEnabled, NTPServers: nonNil(p.NTPServers), Timezone: *p.Timezone,
		},
	}, nil
}

func (a *sudoApplier) repairPossible(ctx context.Context) bool {
	if !payloadPresent(a.payload) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, repairProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-l", a.toolctl, "install-tool", hostctlToolID) //nolint:gosec // fixed paths and id
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	return cmd.Run() == nil
}

func payloadPresent(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, "gotham-hostctl"))
	return err == nil && fi.Mode().IsRegular()
}

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

func networkStackAvailable() bool {
	return toolcatalog.Installed(toolcatalog.IDSystemdNetworkd) &&
		toolcatalog.Installed(toolcatalog.IDSystemdResolved) &&
		toolcatalog.Installed(toolcatalog.IDNetworkctl)
}

func systemStackAvailable() bool {
	if !toolcatalog.Installed(toolcatalog.IDSystemctl) {
		return false
	}
	_, err := exec.LookPath("hostnamectl")
	return err == nil
}

type statusPayload struct {
	Network     bool         `json:"network"`
	System      bool         `json:"system"`
	HostNetwork *hostNetwork `json:"host_network"`
	Hostname    string       `json:"hostname"`
	NTPEnabled  bool         `json:"ntp_enabled"`
	NTPServers  []string     `json:"ntp_servers"`
	// Timezone stays a pointer: a helper that predates it has no timezone key
	// and no timezone key on apply-system either, so it cannot apply it.
	Timezone *string `json:"timezone"`
	// Cleanup is the same outdated signal for the cleanup verbs.
	Cleanup bool `json:"cleanup"`
}

type hostCleanupCategory struct {
	Supported bool   `json:"supported"`
	Manager   string `json:"manager"`
	Bytes     int64  `json:"bytes"`
}

type hostCleanupStatusPayload struct {
	Journal      hostCleanupCategory `json:"journal"`
	PackageCache hostCleanupCategory `json:"package_cache"`
}

type hostCleanupResultPayload struct {
	Before int64 `json:"before"`
	After  int64 `json:"after"`
}

type hostNetwork struct {
	Interface  string     `json:"interface"`
	DNSServers []string   `json:"dns_servers"`
	IPv4       IPConfig   `json:"ipv4"`
	IPv6       IPv6Config `json:"ipv6"`
}

func networkFromHost(h *hostNetwork) Network {
	if h == nil || h.Interface == "" {
		return Network{}
	}
	return Network{DNSServers: nonNil(h.DNSServers), IPv4: h.IPv4, IPv6: h.IPv6}
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
		"ntp_servers=" + strings.Join(s.NTPServers, ",") + "\n" +
		"timezone=" + s.Timezone + "\n"
	_, err := a.run(ctx, "apply-system", in)
	return err
}

func (a *sudoApplier) CleanupStatus(ctx context.Context) (HostCleanupStatus, error) {
	if _, err := os.Stat(a.path); err != nil {
		return HostCleanupStatus{}, ErrHelperOutdated
	}
	status, ok := a.fetchStatus(ctx)
	if !ok {
		return HostCleanupStatus{}, fmt.Errorf("%w: read host helper status", ErrHost)
	}
	if !status.Cleanup {
		return HostCleanupStatus{}, ErrHelperOutdated
	}
	out, err := a.run(ctx, "cleanup-status", "")
	if err != nil {
		return HostCleanupStatus{}, cleanupRunError(err)
	}
	var payload hostCleanupStatusPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil {
		return HostCleanupStatus{}, fmt.Errorf("%w: unreadable cleanup status", ErrHost)
	}
	return HostCleanupStatus{
		Journal: HostCleanupCategory{
			Supported: payload.Journal.Supported, Manager: payload.Journal.Manager, Bytes: payload.Journal.Bytes,
		},
		PackageCache: HostCleanupCategory{
			Supported: payload.PackageCache.Supported, Manager: payload.PackageCache.Manager, Bytes: payload.PackageCache.Bytes,
		},
	}, nil
}

func (a *sudoApplier) CleanupHost(ctx context.Context, category string, minAge time.Duration) (int64, error) {
	var verb, stdin string
	switch category {
	case "journal":
		verb = "cleanup-journal"
		seconds := int64(minAge / time.Second)
		if seconds < 0 {
			seconds = 0
		}
		stdin = "min_age_seconds=" + strconv.FormatInt(seconds, 10) + "\n"
	case "package_cache":
		verb = "cleanup-package-cache"
	default:
		return 0, ErrUnsupported
	}
	out, err := a.run(ctx, verb, stdin)
	if err != nil {
		return 0, cleanupRunError(err)
	}
	var payload hostCleanupResultPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil {
		return 0, fmt.Errorf("%w: unreadable cleanup result", ErrHost)
	}
	if payload.After >= payload.Before {
		return 0, nil
	}
	return payload.Before - payload.After, nil
}
