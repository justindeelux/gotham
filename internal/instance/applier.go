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

type HostNetwork struct {
	Interface  string     `json:"interface"`
	DNSServers []string   `json:"dns_servers"`
	IPv4       IPConfig   `json:"ipv4"`
	IPv6       IPv6Config `json:"ipv6"`
}

type HostApplier interface {
	Probe(ctx context.Context) Host
	InstallHostHelper(ctx context.Context) (string, error)
	HostNetwork(ctx context.Context) (live HostNetwork, ok bool)
	ApplyNetwork(ctx context.Context, net Network, revertAfter time.Duration, dnsOnly bool) error
	ConfirmNetwork(ctx context.Context) error
	RevertNetwork(ctx context.Context) error
	ApplySystem(ctx context.Context, sys System) error
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

func (a *sudoApplier) run(ctx context.Context, verb, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", a.path, verb) //nolint:gosec // fixed path and verbs
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s: %s", ErrHost, verb, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

func (a *sudoApplier) Probe(ctx context.Context) Host {
	h := Host{
		Helper:       HelperReady,
		NetworkStack: networkStackAvailable(),
		SystemStack:  systemStackAvailable(),
	}
	if _, err := os.Stat(a.path); err != nil {
		h.Helper = HelperMissing
		h.Repair = a.repairPossible(ctx)
		return h
	}
	p, ok := a.fetchStatus(ctx)
	if !ok {
		h.Helper = HelperDenied
		h.Repair = a.repairPossible(ctx)
		return h
	}
	h.Network = p.Network
	h.System = p.System
	return h
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
