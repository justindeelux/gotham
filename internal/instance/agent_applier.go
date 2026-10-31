package instance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const agentHostTimeout = 60 * time.Second

// HostClientFunc dials one node's agent HostService. The returned close
// releases the connection when the call is done.
type HostClientFunc func(ctx context.Context) (agentv1.HostServiceClient, func(), error)

type agentApplier struct{ dial HostClientFunc }

// NewAgentApplier returns the applier for a node that is not the control
// plane's own machine: the agent runs the same root-owned helper over its
// verb-pinned sudoers rule.
func NewAgentApplier(dial HostClientFunc) HostApplier {
	return &agentApplier{dial: dial}
}

// client bounds the whole RPC, not only the dial: an offline node must not
// hold a settings request open indefinitely.
func (a *agentApplier) client(ctx context.Context) (context.Context, agentv1.HostServiceClient, func(), error) {
	callCtx, cancel := context.WithTimeout(ctx, agentHostTimeout)
	client, closeFn, err := a.dial(callCtx)
	if err != nil {
		cancel()
		return nil, nil, nil, classifyDialError(err)
	}
	return callCtx, client, func() {
		if closeFn != nil {
			closeFn()
		}
		cancel()
	}, nil
}

func (a *agentApplier) Status(ctx context.Context) (HostStatus, error) {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return HostStatus{}, err
	}
	defer closeFn()
	resp, err := client.Status(callCtx, &agentv1.HostStatusRequest{})
	if err != nil {
		return HostStatus{}, classifyAgentError(err)
	}
	st := HostStatus{
		Capability: capabilityFromAgent(resp.GetCapability()),
		System: System{
			Hostname: resp.GetHostname(), NTPEnabled: resp.GetNtpEnabled(),
			NTPServers: nonNil(resp.GetNtpServers()), Timezone: resp.GetTimezone(),
		},
		AgentRoot: resp.GetAgentRoot(),
	}
	if ssh := resp.GetSsh(); ssh != nil {
		st.SSH = SSHSettings{
			Available:              ssh.GetAvailable(),
			Port:                   ssh.GetPort(),
			PasswordAuthentication: ssh.GetPasswordAuthentication(),
			KeyAuthAvailable:       ssh.GetKeyAuthAvailable(),
		}
	} else if st.Capability == CapabilityReady {
		st.Capability = CapabilityHelperOutdated
	}
	if network := resp.GetNetwork(); network != nil && network.GetInterface() != "" {
		st.Network = Network{
			DNSServers: nonNil(network.GetDnsServers()),
			IPv4: IPConfig{
				Mode: network.GetIpv4().GetMode(), Address: network.GetIpv4().GetAddress(),
				Gateway: network.GetIpv4().GetGateway(),
			},
			IPv6: IPv6Config{
				Enabled: network.GetIpv6().GetEnabled(), Mode: network.GetIpv6().GetMode(),
				Address: network.GetIpv6().GetAddress(), Gateway: network.GetIpv6().GetGateway(),
			},
		}
	}
	return st, nil
}

func (a *agentApplier) InstallHostHelper(context.Context) (string, error) {
	return "", ErrUnsupported
}

func (a *agentApplier) ApplyNetwork(ctx context.Context, n Network, revertAfter time.Duration, dnsOnly bool) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.ApplyNetwork(callCtx, &agentv1.HostApplyNetworkRequest{
		DnsServers: n.DNSServers,
		Ipv4Mode:   n.IPv4.Mode, Ipv4Address: n.IPv4.Address, Ipv4Gateway: n.IPv4.Gateway,
		Ipv6Enabled: n.IPv6.Enabled, Ipv6Mode: n.IPv6.Mode,
		Ipv6Address: n.IPv6.Address, Ipv6Gateway: n.IPv6.Gateway,
		RevertAfter: uint32(revertAfter.Seconds()), DnsOnly: dnsOnly,
	})
	return classifyApplyError("network", err)
}

func (a *agentApplier) ConfirmNetwork(ctx context.Context) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.ConfirmNetwork(callCtx, &agentv1.HostConfirmNetworkRequest{})
	return classifySettleError(err)
}

func (a *agentApplier) RevertNetwork(ctx context.Context) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.RevertNetwork(callCtx, &agentv1.HostRevertNetworkRequest{})
	return classifySettleError(err)
}

func (a *agentApplier) ApplySystem(ctx context.Context, s System) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.ApplySystem(callCtx, &agentv1.HostApplySystemRequest{
		Hostname: s.Hostname, NtpEnabled: s.NTPEnabled, NtpServers: s.NTPServers, Timezone: s.Timezone,
	})
	return classifyApplyError("system", err)
}

func (a *agentApplier) ApplySSH(ctx context.Context, in SSHInput) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.ApplySSH(callCtx, &agentv1.HostApplySSHRequest{
		Port: in.Port, PasswordAuthentication: in.PasswordAuthentication,
	})
	if err != nil && status.Code(err) == codes.FailedPrecondition {
		return fmt.Errorf("%w: %s", ErrUnsupported, status.Convert(err).Message())
	}
	return classifyApplyError("ssh", err)
}

func (a *agentApplier) ConfirmSSH(ctx context.Context) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.ConfirmSSH(callCtx, &agentv1.HostConfirmSSHRequest{})
	return classifySettleError(err)
}

func (a *agentApplier) RevertSSH(ctx context.Context) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.RevertSSH(callCtx, &agentv1.HostRevertSSHRequest{})
	return classifySettleError(err)
}

func (a *agentApplier) ApplyAgentRoot(ctx context.Context, enabled bool) error {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	_, err = client.ApplyAgentRoot(callCtx, &agentv1.HostApplyAgentRootRequest{Enabled: enabled})
	if err != nil && status.Code(err) == codes.FailedPrecondition {
		return fmt.Errorf("%w: %s", ErrUnsupported, status.Convert(err).Message())
	}
	return classifyAgentError(err)
}

func (a *agentApplier) CleanupStatus(ctx context.Context) (HostCleanupStatus, error) {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return HostCleanupStatus{}, err
	}
	defer closeFn()
	resp, err := client.CleanupStatus(callCtx, &agentv1.HostCleanupStatusRequest{})
	if err != nil {
		return HostCleanupStatus{}, classifyAgentError(err)
	}
	if resp.GetCapability() != agentv1.HostCapability_HOST_CAPABILITY_READY {
		return HostCleanupStatus{}, ErrHelperOutdated
	}
	return HostCleanupStatus{
		Journal: HostCleanupCategory{
			Supported: resp.GetJournal().GetSupported(),
			Manager:   resp.GetJournal().GetManager(),
			Bytes:     resp.GetJournal().GetBytes(),
		},
		PackageCache: HostCleanupCategory{
			Supported: resp.GetPackageCache().GetSupported(),
			Manager:   resp.GetPackageCache().GetManager(),
			Bytes:     resp.GetPackageCache().GetBytes(),
		},
	}, nil
}

func (a *agentApplier) CleanupHost(ctx context.Context, category string, minAge time.Duration) (int64, error) {
	callCtx, client, closeFn, err := a.client(ctx)
	if err != nil {
		return 0, err
	}
	defer closeFn()
	seconds := int64(minAge / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	resp, err := client.CleanupHost(callCtx, &agentv1.HostCleanupRequest{
		Category:      category,
		MinAgeSeconds: uint64(seconds),
	})
	if err != nil {
		switch status.Code(err) {
		case codes.FailedPrecondition:
			return 0, fmt.Errorf("%w: %s", ErrUnsupported, status.Convert(err).Message())
		case codes.InvalidArgument:
			// The agent fixes the key set itself, so a rejection here is an
			// older helper that does not know the cleanup verb.
			return 0, fmt.Errorf("%w: %v", ErrHelperOutdated, err)
		}
		return 0, classifyAgentError(err)
	}
	return resp.GetReclaimedBytes(), nil
}

func capabilityFromAgent(c agentv1.HostCapability) NodeCapability {
	switch c {
	case agentv1.HostCapability_HOST_CAPABILITY_READY:
		return CapabilityReady
	case agentv1.HostCapability_HOST_CAPABILITY_UNSUPPORTED_STACK:
		return CapabilityUnsupportedStack
	case agentv1.HostCapability_HOST_CAPABILITY_HELPER_OUTDATED:
		return CapabilityHelperOutdated
	case agentv1.HostCapability_HOST_CAPABILITY_HELPER_MISSING:
		return CapabilityHelperMissing
	case agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED:
		return CapabilityHelperDenied
	}
	return CapabilityAgentOutdated
}

// classifyDialError handles failures before any RPC: no dialable address or a
// rejected certificate has no gRPC status, so it must still read as offline
// instead of a host-helper failure.
func classifyDialError(err error) error {
	if status.Code(err) == codes.Unknown {
		return ErrNodeOffline
	}
	return classifyAgentError(err)
}

// classifyAgentError turns a transport failure into the capability the UI
// needs: an agent that predates HostService is outdated, an unreachable one is
// offline, and anything else is a helper failure.
func classifyAgentError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Unimplemented:
		return ErrAgentOutdated
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return ErrNodeOffline
	}
	return errors.Join(ErrHost, err)
}

// classifyApplyError keeps the pending/unsupported refusals distinct from a
// helper crash so the API can answer with a clear conflict.
func classifyApplyError(what string, err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return fieldsFor(what, "the host rejected the value")
	}
	return classifyAgentError(err)
}

func classifySettleError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.FailedPrecondition:
		return ErrNoPending
	}
	return classifyAgentError(err)
}

func fieldsFor(what, msg string) error {
	switch what {
	case "network":
		return FieldErrors{"network": msg}
	case "ssh":
		return FieldErrors{"port": msg}
	default:
		return FieldErrors{"system": msg}
	}
}
