package instance

import (
	"context"
	"errors"
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
		return nil, nil, nil, classifyAgentError(err)
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

func capabilityFromAgent(c agentv1.HostCapability) NodeCapability {
	switch c {
	case agentv1.HostCapability_HOST_CAPABILITY_READY:
		return CapabilityReady
	case agentv1.HostCapability_HOST_CAPABILITY_UNSUPPORTED_STACK:
		return CapabilityUnsupportedStack
	case agentv1.HostCapability_HOST_CAPABILITY_HELPER_MISSING:
		return CapabilityHelperMissing
	case agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED:
		return CapabilityHelperDenied
	}
	return CapabilityAgentOutdated
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
	if what == "network" {
		return FieldErrors{"network": msg}
	}
	return FieldErrors{"system": msg}
}
