package instance

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeHostClient struct {
	agentv1.HostServiceClient
	status    *agentv1.HostStatusResponse
	statusErr error
	applied   *agentv1.HostApplyNetworkRequest
	applyErr  error
	system    *agentv1.HostApplySystemRequest
	systemErr error

	cleanupStatus    *agentv1.HostCleanupStatus
	cleanupStatusErr error
	cleanupRequest   *agentv1.HostCleanupRequest
	cleanupResult    *agentv1.HostCleanupResult
	cleanupErr       error
}

func (f *fakeHostClient) Status(context.Context, *agentv1.HostStatusRequest, ...grpc.CallOption) (*agentv1.HostStatusResponse, error) {
	return f.status, f.statusErr
}

func (f *fakeHostClient) ApplyNetwork(_ context.Context, in *agentv1.HostApplyNetworkRequest, _ ...grpc.CallOption) (*agentv1.HostApplyNetworkResponse, error) {
	f.applied = in
	return &agentv1.HostApplyNetworkResponse{}, f.applyErr
}

func (f *fakeHostClient) ConfirmNetwork(context.Context, *agentv1.HostConfirmNetworkRequest, ...grpc.CallOption) (*agentv1.HostConfirmNetworkResponse, error) {
	return &agentv1.HostConfirmNetworkResponse{}, nil
}

func (f *fakeHostClient) RevertNetwork(context.Context, *agentv1.HostRevertNetworkRequest, ...grpc.CallOption) (*agentv1.HostRevertNetworkResponse, error) {
	return &agentv1.HostRevertNetworkResponse{}, nil
}

func (f *fakeHostClient) ApplySystem(_ context.Context, in *agentv1.HostApplySystemRequest, _ ...grpc.CallOption) (*agentv1.HostApplySystemResponse, error) {
	f.system = in
	return &agentv1.HostApplySystemResponse{}, f.systemErr
}

func (f *fakeHostClient) CleanupStatus(context.Context, *agentv1.HostCleanupStatusRequest, ...grpc.CallOption) (*agentv1.HostCleanupStatus, error) {
	return f.cleanupStatus, f.cleanupStatusErr
}

func (f *fakeHostClient) CleanupHost(_ context.Context, in *agentv1.HostCleanupRequest, _ ...grpc.CallOption) (*agentv1.HostCleanupResult, error) {
	f.cleanupRequest = in
	return f.cleanupResult, f.cleanupErr
}

func newAgentApplier(fake *fakeHostClient) HostApplier {
	return NewAgentApplier(func(context.Context) (agentv1.HostServiceClient, func(), error) {
		return fake, func() {}, nil
	})
}

func TestAgentApplierMapsStatus(t *testing.T) {
	fake := &fakeHostClient{status: &agentv1.HostStatusResponse{
		Capability: agentv1.HostCapability_HOST_CAPABILITY_READY,
		Network: &agentv1.HostNetworkConfig{
			Interface: "eth0", DnsServers: []string{"1.1.1.1"},
			Ipv4: &agentv1.HostIPv4Config{Mode: ModeDHCP},
			Ipv6: &agentv1.HostIPv6Config{Enabled: true, Mode: ModeDHCP},
		},
		Hostname: "node-a", NtpEnabled: true, NtpServers: []string{"time.cloudflare.com"}, Timezone: "Europe/Berlin",
	}}
	st, err := newAgentApplier(fake).Status(context.Background())
	if err != nil || st.Capability != CapabilityReady {
		t.Fatalf("status: %+v %v", st, err)
	}
	if st.System.Timezone != "Europe/Berlin" || st.System.Hostname != "node-a" || !st.System.NTPEnabled {
		t.Fatalf("system not mapped: %+v", st.System)
	}
	if st.Network.IPv4.Mode != ModeDHCP || st.Network.DNSServers[0] != "1.1.1.1" {
		t.Fatalf("network not mapped: %+v", st.Network)
	}
}

func TestAgentApplierClassifiesTransportFailures(t *testing.T) {
	outdated := &fakeHostClient{statusErr: status.Error(codes.Unimplemented, "no host service")}
	if _, err := newAgentApplier(outdated).Status(context.Background()); !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("unimplemented = %v, want agent_outdated", err)
	}
	offline := &fakeHostClient{statusErr: status.Error(codes.Unavailable, "down")}
	if _, err := newAgentApplier(offline).Status(context.Background()); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("unavailable = %v, want offline", err)
	}
}

func TestAgentApplierCleanupStatus(t *testing.T) {
	fake := &fakeHostClient{cleanupStatus: &agentv1.HostCleanupStatus{
		Capability: agentv1.HostCapability_HOST_CAPABILITY_READY,
		Journal:    &agentv1.HostCleanupCategoryStatus{Supported: true, Bytes: 100},
		PackageCache: &agentv1.HostCleanupCategoryStatus{
			Supported: true, Manager: "apt", Bytes: 50,
		},
	}}
	cleanup, err := newAgentApplier(fake).CleanupStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cleanup.Journal.Supported || cleanup.Journal.Bytes != 100 ||
		!cleanup.PackageCache.Supported || cleanup.PackageCache.Manager != "apt" || cleanup.PackageCache.Bytes != 50 {
		t.Fatalf("cleanup status not mapped: %+v", cleanup)
	}

	outdated := &fakeHostClient{cleanupStatus: &agentv1.HostCleanupStatus{
		Capability: agentv1.HostCapability_HOST_CAPABILITY_HELPER_OUTDATED,
	}}
	if _, err := newAgentApplier(outdated).CleanupStatus(context.Background()); !errors.Is(err, ErrHelperOutdated) {
		t.Fatalf("outdated helper = %v, want helper outdated", err)
	}
	unimplemented := &fakeHostClient{cleanupStatusErr: status.Error(codes.Unimplemented, "old agent")}
	if _, err := newAgentApplier(unimplemented).CleanupStatus(context.Background()); !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("old agent = %v, want agent outdated", err)
	}
}

func TestAgentApplierCleanupHost(t *testing.T) {
	fake := &fakeHostClient{cleanupResult: &agentv1.HostCleanupResult{ReclaimedBytes: 60}}
	reclaimed, err := newAgentApplier(fake).CleanupHost(context.Background(), "journal", 2*time.Minute)
	if err != nil || reclaimed != 60 {
		t.Fatalf("cleanup host = %d, %v", reclaimed, err)
	}
	if fake.cleanupRequest.GetCategory() != "journal" || fake.cleanupRequest.GetMinAgeSeconds() != 120 {
		t.Fatalf("cleanup request = %+v", fake.cleanupRequest)
	}

	unsupported := &fakeHostClient{cleanupErr: status.Error(codes.FailedPrecondition, "unsupported")}
	if _, err := newAgentApplier(unsupported).CleanupHost(context.Background(), "journal", 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported = %v", err)
	}
	old := &fakeHostClient{cleanupErr: status.Error(codes.InvalidArgument, "the host helper rejected the value")}
	if _, err := newAgentApplier(old).CleanupHost(context.Background(), "journal", 0); !errors.Is(err, ErrHelperOutdated) {
		t.Fatalf("old helper = %v, want helper outdated", err)
	}
	unimplemented := &fakeHostClient{cleanupErr: status.Error(codes.Unimplemented, "old agent")}
	if _, err := newAgentApplier(unimplemented).CleanupHost(context.Background(), "journal", 0); !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("old agent = %v, want agent outdated", err)
	}
}

func TestAgentApplierDialFailureIsOffline(t *testing.T) {
	noAddress := NewAgentApplier(func(context.Context) (agentv1.HostServiceClient, func(), error) {
		return nil, nil, errors.New("servers: validation failed: server has no dialable address")
	})
	if _, err := noAddress.Status(context.Background()); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("dial failure = %v, want offline", err)
	}
	if err := noAddress.ApplySystem(context.Background(), System{NTPEnabled: true}); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("dial failure on apply = %v, want offline", err)
	}

	denied := NewAgentApplier(func(context.Context) (agentv1.HostServiceClient, func(), error) {
		return nil, nil, status.Error(codes.PermissionDenied, "bad client certificate")
	})
	if _, err := denied.Status(context.Background()); !errors.Is(err, ErrHost) {
		t.Fatalf("credential refusal = %v, want host failure", err)
	}
}

func TestAgentApplierAppliesValues(t *testing.T) {
	fake := &fakeHostClient{}
	applier := newAgentApplier(fake)
	if err := applier.ApplyNetwork(context.Background(), goodNet(), NetworkConfirmWindow, true); err != nil {
		t.Fatal(err)
	}
	if fake.applied.GetRevertAfter() != uint32(NetworkConfirmWindow.Seconds()) || !fake.applied.GetDnsOnly() {
		t.Fatalf("apply request: %+v", fake.applied)
	}
	if fake.applied.GetIpv4Address() != goodNet().IPv4.Address {
		t.Fatalf("address not forwarded: %+v", fake.applied)
	}
	if err := applier.ApplySystem(context.Background(), System{Hostname: "node-a", NTPEnabled: true, Timezone: "Europe/Berlin"}); err != nil {
		t.Fatal(err)
	}
	if fake.system.GetTimezone() != "Europe/Berlin" || fake.system.GetHostname() != "node-a" {
		t.Fatalf("system request: %+v", fake.system)
	}
	if _, err := applier.InstallHostHelper(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("remote helper install = %v, want unsupported", err)
	}
}

func TestAgentApplierSettleRefusals(t *testing.T) {
	fail := &fakeHostClient{applyErr: status.Error(codes.FailedPrecondition, "unsupported")}
	if err := newAgentApplier(fail).ApplyNetwork(context.Background(), goodNet(), time.Minute, false); !errors.Is(err, ErrHost) {
		t.Fatalf("apply refusal = %v, want host failure", err)
	}
}
