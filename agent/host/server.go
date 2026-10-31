// Package host implements the agent's HostService: it runs the fixed,
// root-owned gotham-hostctl helper through sudo -n on the node the agent runs
// on and reports what that host supports. The helper re-validates every value
// and owns the file writes and the network revert timer, so this package is a
// transport only and never parses or trusts values itself.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HelperPath is the fixed, root-owned helper. The agent may run exactly its
// verbs through the sudoers drop-in installed by the agent installer.
const HelperPath = "/usr/libexec/gotham/gotham-hostctl"

const helperTimeout = 60 * time.Second

type runner interface {
	Run(ctx context.Context, verb, stdin string) (string, error)
}

// runError carries the helper's exit code and its own last diagnostic line.
// Underlying command stderr is dropped: it may name paths, while the helper
// only ever writes its fixed "gotham-hostctl: ..." die lines itself.
type runError struct {
	verb   string
	code   int
	err    error
	reason string
}

func (e *runError) Error() string {
	return fmt.Sprintf("gotham-hostctl %s failed (exit %d)", e.verb, e.code)
}

func (e *runError) Unwrap() error { return e.err }

// helperReasonLine returns the helper's own last diagnostic line, bounded, and
// never a line from a command it ran.
func helperReasonLine(stderr string) string {
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

type sudoRunner struct{ helper string }

func (r sudoRunner) Run(ctx context.Context, verb, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "-n", r.helper, verb) //nolint:gosec // fixed path and verb
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return "", &runError{verb: verb, code: code, err: err, reason: helperReasonLine(errOut.String())}
	}
	return out.String(), nil
}

type Server struct {
	agentv1.UnimplementedHostServiceServer

	helper string
	runner runner
}

type Option func(*Server)

// WithHelper overrides the helper path (tests and non-default layouts).
func WithHelper(path string) Option {
	return func(s *Server) { s.helper = path }
}

// NewServer returns the node's host service.
func NewServer(options ...Option) *Server {
	server := &Server{helper: HelperPath}
	for _, option := range options {
		option(server)
	}
	if server.runner == nil {
		server.runner = sudoRunner{helper: server.helper}
	}
	return server
}

type statusPayload struct {
	Network     bool           `json:"network"`
	System      bool           `json:"system"`
	HostNetwork *networkConfig `json:"host_network"`
	Hostname    string         `json:"hostname"`
	NTPEnabled  bool           `json:"ntp_enabled"`
	NTPServers  []string       `json:"ntp_servers"`
	// Timezone stays a pointer: a helper that predates it has no timezone key,
	// which is how an outdated helper is told apart from a missing stack.
	Timezone *string `json:"timezone"`
	// Cleanup is the same signal for the cleanup verbs: a helper that predates
	// them has no key, so it must be updated before a cleanup can run.
	Cleanup bool `json:"cleanup"`
	// SSH and AgentRoot are pointers for the same reason: a helper that
	// predates SSH settings has neither key.
	SSH       *sshStatus `json:"ssh"`
	AgentRoot *bool      `json:"agent_root"`
}

// sshStatus is the helper's SSH section. The zero value means sshd is not
// installed; the pointer in statusPayload tells an old helper apart.
type sshStatus struct {
	Available              bool   `json:"available"`
	Port                   uint32 `json:"port"`
	PasswordAuthentication bool   `json:"password_authentication"`
	KeyAuthAvailable       bool   `json:"key_auth_available"`
}

// cleanupStatusPayload is the helper's `cleanup-status` document. Unknown
// fields are ignored so a newer helper stays readable.
type cleanupStatusPayload struct {
	Journal      cleanupCategoryStatus `json:"journal"`
	PackageCache cleanupCategoryStatus `json:"package_cache"`
}

type cleanupCategoryStatus struct {
	Supported bool   `json:"supported"`
	Bytes     int64  `json:"bytes"`
	Manager   string `json:"manager"`
}

type cleanupResultPayload struct {
	Before int64 `json:"before"`
	After  int64 `json:"after"`
}

type networkConfig struct {
	Interface  string     `json:"interface"`
	DNSServers []string   `json:"dns_servers"`
	IPv4       ipv4Config `json:"ipv4"`
	IPv6       ipv6Config `json:"ipv6"`
}

type ipv4Config struct {
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

type ipv6Config struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
}

func (s *Server) Status(ctx context.Context, _ *agentv1.HostStatusRequest) (*agentv1.HostStatusResponse, error) {
	if _, err := os.Stat(s.helper); err != nil {
		return &agentv1.HostStatusResponse{
			Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_MISSING,
		}, nil
	}
	out, err := s.runner.Run(ctx, "status", "")
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, status.FromContextError(ctxErr).Err()
	}
	if err != nil {
		return &agentv1.HostStatusResponse{
			Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED,
		}, nil
	}
	var payload statusPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil {
		// The helper's status is a fixed JSON document; anything else is not
		// the helper we expect, so it is reported like a refused call.
		return &agentv1.HostStatusResponse{
			Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED,
		}, nil
	}
	response := &agentv1.HostStatusResponse{
		Capability: agentv1.HostCapability_HOST_CAPABILITY_READY,
		Hostname:   payload.Hostname,
		NtpEnabled: payload.NTPEnabled,
		NtpServers: payload.NTPServers,
	}
	if payload.Timezone != nil {
		response.Timezone = *payload.Timezone
	}
	if payload.SSH != nil {
		response.Ssh = &agentv1.HostSSHSettings{
			Available:              payload.SSH.Available,
			Port:                   payload.SSH.Port,
			PasswordAuthentication: payload.SSH.PasswordAuthentication,
			KeyAuthAvailable:       payload.SSH.KeyAuthAvailable,
		}
	}
	if payload.AgentRoot != nil {
		response.AgentRoot = *payload.AgentRoot
	}
	switch {
	case payload.Timezone == nil || payload.SSH == nil || payload.AgentRoot == nil:
		response.Capability = agentv1.HostCapability_HOST_CAPABILITY_HELPER_OUTDATED
	case !payload.Network || !payload.System:
		// The platform manages network and system settings together, so a host
		// that lacks either stack is reported as unsupported.
		response.Capability = agentv1.HostCapability_HOST_CAPABILITY_UNSUPPORTED_STACK
	}
	if payload.Network && payload.HostNetwork != nil && payload.HostNetwork.Interface != "" {
		response.Network = &agentv1.HostNetworkConfig{
			Interface:  payload.HostNetwork.Interface,
			DnsServers: payload.HostNetwork.DNSServers,
			Ipv4: &agentv1.HostIPv4Config{
				Mode:    payload.HostNetwork.IPv4.Mode,
				Address: payload.HostNetwork.IPv4.Address,
				Gateway: payload.HostNetwork.IPv4.Gateway,
			},
			Ipv6: &agentv1.HostIPv6Config{
				Enabled: payload.HostNetwork.IPv6.Enabled,
				Mode:    payload.HostNetwork.IPv6.Mode,
				Address: payload.HostNetwork.IPv6.Address,
				Gateway: payload.HostNetwork.IPv6.Gateway,
			},
		}
	}
	return response, nil
}

func (s *Server) ApplyNetwork(ctx context.Context, request *agentv1.HostApplyNetworkRequest) (*agentv1.HostApplyNetworkResponse, error) {
	var b strings.Builder
	line := func(key, value string) { b.WriteString(key + "=" + value + "\n") }
	line("dns", strings.Join(request.GetDnsServers(), ","))
	line("ipv4_mode", request.GetIpv4Mode())
	line("ipv4_address", request.GetIpv4Address())
	line("ipv4_gateway", request.GetIpv4Gateway())
	line("ipv6_enabled", strconv.FormatBool(request.GetIpv6Enabled()))
	line("ipv6_mode", request.GetIpv6Mode())
	line("ipv6_address", request.GetIpv6Address())
	line("ipv6_gateway", request.GetIpv6Gateway())
	line("revert_after", strconv.FormatUint(uint64(request.GetRevertAfter()), 10))
	line("dns_only", strconv.FormatBool(request.GetDnsOnly()))
	if _, err := s.runner.Run(ctx, "apply-network", b.String()); err != nil {
		return nil, helperStatus("apply-network", err)
	}
	return &agentv1.HostApplyNetworkResponse{}, nil
}

func (s *Server) ConfirmNetwork(ctx context.Context, _ *agentv1.HostConfirmNetworkRequest) (*agentv1.HostConfirmNetworkResponse, error) {
	if _, err := s.runner.Run(ctx, "confirm-network", ""); err != nil {
		return nil, helperStatus("confirm-network", err)
	}
	return &agentv1.HostConfirmNetworkResponse{}, nil
}

func (s *Server) RevertNetwork(ctx context.Context, _ *agentv1.HostRevertNetworkRequest) (*agentv1.HostRevertNetworkResponse, error) {
	if _, err := s.runner.Run(ctx, "revert-network", ""); err != nil {
		return nil, helperStatus("revert-network", err)
	}
	return &agentv1.HostRevertNetworkResponse{}, nil
}

func (s *Server) ApplySystem(ctx context.Context, request *agentv1.HostApplySystemRequest) (*agentv1.HostApplySystemResponse, error) {
	in := "hostname=" + request.GetHostname() + "\n" +
		"ntp_enabled=" + strconv.FormatBool(request.GetNtpEnabled()) + "\n" +
		"ntp_servers=" + strings.Join(request.GetNtpServers(), ",") + "\n" +
		"timezone=" + request.GetTimezone() + "\n"
	if _, err := s.runner.Run(ctx, "apply-system", in); err != nil {
		return nil, helperStatus("apply-system", err)
	}
	return &agentv1.HostApplySystemResponse{}, nil
}

// CleanupStatus reports the root-only cleanup categories this host supports.
// A helper that predates the cleanup verbs has no cleanup key in its status,
// which is reported as an outdated helper so the control plane can hint the
// same repair as host settings.
func (s *Server) CleanupStatus(ctx context.Context, _ *agentv1.HostCleanupStatusRequest) (*agentv1.HostCleanupStatus, error) {
	if _, err := os.Stat(s.helper); err != nil {
		return &agentv1.HostCleanupStatus{Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_MISSING}, nil
	}
	out, err := s.runner.Run(ctx, "status", "")
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, status.FromContextError(ctxErr).Err()
	}
	if err != nil {
		return &agentv1.HostCleanupStatus{Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED}, nil
	}
	var host statusPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &host) != nil {
		return &agentv1.HostCleanupStatus{Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED}, nil
	}
	if !host.Cleanup {
		return &agentv1.HostCleanupStatus{Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_OUTDATED}, nil
	}
	out, err = s.runner.Run(ctx, "cleanup-status", "")
	if err != nil {
		return nil, helperStatus("cleanup-status", err)
	}
	var payload cleanupStatusPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil {
		return nil, status.Error(codes.Internal, "the host helper returned an unreadable cleanup status")
	}
	return &agentv1.HostCleanupStatus{
		Capability: agentv1.HostCapability_HOST_CAPABILITY_READY,
		Journal: &agentv1.HostCleanupCategoryStatus{
			Supported: payload.Journal.Supported, Bytes: payload.Journal.Bytes, Manager: payload.Journal.Manager,
		},
		PackageCache: &agentv1.HostCleanupCategoryStatus{
			Supported: payload.PackageCache.Supported, Bytes: payload.PackageCache.Bytes, Manager: payload.PackageCache.Manager,
		},
	}, nil
}

// CleanupHost reclaims one root-only cleanup category. The helper validates
// the value again; this method only fixes the key set and the verb.
func (s *Server) CleanupHost(ctx context.Context, request *agentv1.HostCleanupRequest) (*agentv1.HostCleanupResult, error) {
	var verb, stdin string
	switch request.GetCategory() {
	case "journal":
		verb = "cleanup-journal"
		stdin = "min_age_seconds=" + strconv.FormatUint(request.GetMinAgeSeconds(), 10) + "\n"
	case "package_cache":
		verb = "cleanup-package-cache"
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown cleanup category")
	}
	out, err := s.runner.Run(ctx, verb, stdin)
	if err != nil {
		return nil, helperStatus(verb, err)
	}
	var payload cleanupResultPayload
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil {
		return nil, status.Error(codes.Internal, "the host helper returned an unreadable cleanup result")
	}
	reclaimed := payload.Before - payload.After
	if reclaimed < 0 {
		reclaimed = 0
	}
	return &agentv1.HostCleanupResult{ReclaimedBytes: reclaimed}, nil
}

// ApplySSH changes the SSH port and password login. A port change stays
// tentative on the host until ConfirmSSH, so the old port remains reachable.
func (s *Server) ApplySSH(ctx context.Context, request *agentv1.HostApplySSHRequest) (*agentv1.HostApplySSHResponse, error) {
	in := "port=" + strconv.FormatUint(uint64(request.GetPort()), 10) + "\n" +
		"password_authentication=" + strconv.FormatBool(request.GetPasswordAuthentication()) + "\n"
	if _, err := s.runner.Run(ctx, "apply-ssh", in); err != nil {
		return nil, helperStatus("apply-ssh", err)
	}
	return &agentv1.HostApplySSHResponse{}, nil
}

func (s *Server) ConfirmSSH(ctx context.Context, _ *agentv1.HostConfirmSSHRequest) (*agentv1.HostConfirmSSHResponse, error) {
	if _, err := s.runner.Run(ctx, "confirm-ssh", ""); err != nil {
		return nil, helperStatus("confirm-ssh", err)
	}
	return &agentv1.HostConfirmSSHResponse{}, nil
}

func (s *Server) RevertSSH(ctx context.Context, _ *agentv1.HostRevertSSHRequest) (*agentv1.HostRevertSSHResponse, error) {
	if _, err := s.runner.Run(ctx, "revert-ssh", ""); err != nil {
		return nil, helperStatus("revert-ssh", err)
	}
	return &agentv1.HostRevertSSHResponse{}, nil
}

// ApplyAgentRoot switches the agent between the unprivileged service user and
// root. The helper schedules the restart, so this call returns before the
// agent drops its connection.
func (s *Server) ApplyAgentRoot(ctx context.Context, request *agentv1.HostApplyAgentRootRequest) (*agentv1.HostApplyAgentRootResponse, error) {
	in := "enabled=" + strconv.FormatBool(request.GetEnabled()) + "\n"
	if _, err := s.runner.Run(ctx, "apply-agent-root", in); err != nil {
		return nil, helperStatus("apply-agent-root", err)
	}
	return &agentv1.HostApplyAgentRootResponse{}, nil
}

// helperStatus maps the helper's pinned exit codes to gRPC statuses. The
// helper's own fixed reason line rides along with an unexpected failure; raw
// stderr (which may name host paths) is never forwarded.
func helperStatus(verb string, err error) error {
	var helperErr *runError
	if !errors.As(err, &helperErr) {
		return status.Error(codes.Internal, "the host helper could not be run")
	}
	switch helperErr.code {
	case 2:
		return status.Error(codes.InvalidArgument, "the host helper rejected the value")
	case 3:
		if strings.HasPrefix(verb, "cleanup-") {
			return status.Error(codes.FailedPrecondition, "unsupported")
		}
		if (verb == "apply-ssh" || verb == "apply-agent-root") && helperErr.reason != "" {
			return status.Error(codes.FailedPrecondition, helperErr.reason)
		}
		return status.Error(codes.FailedPrecondition, "this host does not support the setting")
	case 4:
		switch verb {
		case "apply-network":
			return status.Error(codes.FailedPrecondition, "a network change is already pending")
		case "apply-ssh":
			return status.Error(codes.FailedPrecondition, "an SSH change is already pending")
		case "confirm-ssh", "revert-ssh":
			return status.Error(codes.FailedPrecondition, "no SSH change is pending")
		}
		return status.Error(codes.FailedPrecondition, "no network change is pending")
	case 5:
		if helperErr.reason != "" {
			return status.Error(codes.FailedPrecondition, helperErr.reason)
		}
		return status.Error(codes.FailedPrecondition, "the host refused the change")
	}
	if helperErr.reason != "" {
		return status.Error(codes.Internal, "the host helper failed: "+helperErr.reason)
	}
	return status.Error(codes.Internal, "the host helper failed")
}
