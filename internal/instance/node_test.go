package instance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeHost struct {
	status     HostStatus
	statusErr  error
	installLog string
	installErr error
	applyErr   error
	installs   int
	applied    int
	confirmed  int
	reverts    int
	dnsOnlys   []bool
	lastSystem System

	cleanupStatus HostCleanupStatus
	cleanupErr    error
	reclaimed     int64
	cleaned       []string

	sshApplied    []SSHInput
	sshUsers      []string
	sshConfirmed  int
	sshReverted   int
	sshApplyErr   error
	sshConfirmErr error
	agentApplied  []bool
	agentStays    bool
}

func (h *fakeHost) Status(context.Context) (HostStatus, error) {
	if h.statusErr != nil {
		return HostStatus{}, h.statusErr
	}
	if h.status.Capability == "" {
		h.status.Capability = CapabilityReady
		h.status.Repair = true
	}
	return h.status, nil
}

func (h *fakeHost) InstallHostHelper(context.Context) (string, error) {
	h.installs++
	return h.installLog, h.installErr
}

func (h *fakeHost) ApplyNetwork(_ context.Context, _ Network, _ time.Duration, dnsOnly bool) error {
	h.applied++
	h.dnsOnlys = append(h.dnsOnlys, dnsOnly)
	return h.applyErr
}

func (h *fakeHost) ConfirmNetwork(context.Context) error { h.confirmed++; return nil }
func (h *fakeHost) RevertNetwork(context.Context) error  { h.reverts++; return nil }
func (h *fakeHost) ApplySystem(_ context.Context, in System) error {
	h.lastSystem = in
	return h.applyErr
}

func (h *fakeHost) ApplySSH(_ context.Context, in SSHInput, sshUser string) error {
	h.sshApplied = append(h.sshApplied, in)
	h.sshUsers = append(h.sshUsers, sshUser)
	return h.sshApplyErr
}

func (h *fakeHost) ConfirmSSH(context.Context) error {
	h.sshConfirmed++
	return h.sshConfirmErr
}
func (h *fakeHost) RevertSSH(context.Context) error { h.sshReverted++; return nil }
func (h *fakeHost) ApplyAgentRoot(_ context.Context, enabled bool) error {
	h.agentApplied = append(h.agentApplied, enabled)
	if !h.agentStays {
		h.status.AgentRoot = enabled
	}
	return nil
}

func (h *fakeHost) CleanupStatus(context.Context) (HostCleanupStatus, error) {
	return h.cleanupStatus, h.cleanupErr
}

func (h *fakeHost) CleanupHost(_ context.Context, category string, _ time.Duration) (int64, error) {
	h.cleaned = append(h.cleaned, category)
	return h.reclaimed, h.cleanupErr
}

type fakeNodeRepo struct {
	pending map[uuid.UUID]Pending
	audits  []string
	address string
	sshUser string
	port    int
	ports   []int
	portErr error
}

func newFakeNodeRepo() *fakeNodeRepo { return &fakeNodeRepo{pending: map[uuid.UUID]Pending{}} }

func (r *fakeNodeRepo) Pending(_ context.Context, id uuid.UUID) (*Pending, error) {
	p, ok := r.pending[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (r *fakeNodeRepo) BeginPending(_ context.Context, id uuid.UUID, pending Pending) (bool, error) {
	if _, ok := r.pending[id]; ok {
		return false, nil
	}
	r.pending[id] = pending
	return true, nil
}

func (r *fakeNodeRepo) ClearPending(_ context.Context, id uuid.UUID) error {
	delete(r.pending, id)
	return nil
}

func (r *fakeNodeRepo) Audit(_ context.Context, _ uuid.UUID, section string, _ any) error {
	r.audits = append(r.audits, section)
	return nil
}

func (r *fakeNodeRepo) ServerAddress(context.Context, uuid.UUID) (string, error) {
	if r.address == "" {
		return "127.0.0.1", nil
	}
	return r.address, nil
}

func (r *fakeNodeRepo) ServerSSHUser(context.Context, uuid.UUID) (string, error) {
	if r.sshUser == "" {
		return "root", nil
	}
	return r.sshUser, nil
}

func (r *fakeNodeRepo) SetSSHPort(_ context.Context, _ uuid.UUID, port int) error {
	if r.portErr != nil {
		return r.portErr
	}
	r.port = port
	r.ports = append(r.ports, port)
	return nil
}

type fakeNodes struct{ hosts map[uuid.UUID]NodeHost }

func (n fakeNodes) NodeHost(_ context.Context, id uuid.UUID) (NodeHost, error) {
	host, ok := n.hosts[id]
	if !ok {
		return NodeHost{}, ErrNodeNotFound
	}
	return host, nil
}

func goodNet() Network {
	return Network{
		DNSServers: []string{"1.1.1.1", "2606:4700:4700::1111"},
		IPv4:       IPConfig{Mode: ModeStatic, Address: "10.0.0.5/24", Gateway: "10.0.0.1"},
		IPv6:       IPv6Config{Enabled: true, Mode: ModeDHCP},
	}
}

func staticLive() Network {
	return Network{
		DNSServers: []string{"1.1.1.1"},
		IPv4:       IPConfig{Mode: ModeStatic, Address: "103.176.22.225/24", Gateway: "103.176.22.1"},
		IPv6:       IPv6Config{Enabled: false, Mode: ModeDHCP},
	}
}

type nodeFixture struct {
	svc     *NodeService
	repo    *fakeNodeRepo
	nodes   fakeNodes
	general *Service
	servers []uuid.UUID
	hosts   []*fakeHost
}

func newNodeFixture(t *testing.T, host *fakeHost, general *Service) *nodeFixture {
	t.Helper()
	repo := newFakeNodeRepo()
	nodes := fakeNodes{hosts: map[uuid.UUID]NodeHost{}}
	f := &nodeFixture{repo: repo, nodes: nodes, general: general}
	id := uuid.New()
	f.servers = append(f.servers, id)
	f.hosts = append(f.hosts, host)
	nodes.hosts[id] = NodeHost{Applier: host}
	if general == nil {
		general = NewService(&fakeRepo{}, quietLogger())
		general.lookup = func(string) string { return "" }
		f.general = general
	}
	f.svc = NewNodeService(repo, nodes, general, quietLogger())
	f.svc.now = time.Now
	return f
}

func TestNodeNetworkConfirmAndRevert(t *testing.T) {
	host := &fakeHost{status: HostStatus{Network: goodNet()}}
	f := newNodeFixture(t, host, nil)
	ctx := context.Background()
	id := f.servers[0]
	now := time.Now()
	f.svc.now = func() time.Time { return now }

	st, err := f.svc.UpdateNetwork(ctx, uuid.Nil, id, NetworkInput{Network: goodNet()})
	if err != nil || st.Pending == nil || host.applied != 1 {
		t.Fatalf("apply: %+v %v", st, err)
	}
	if _, err := f.svc.UpdateNetwork(ctx, uuid.Nil, id, NetworkInput{Network: goodNet()}); !errors.Is(err, ErrPending) {
		t.Fatalf("second change while pending: %v", err)
	}
	if st, err = f.svc.ConfirmNetwork(ctx, uuid.Nil, id); err != nil || st.Pending != nil || host.confirmed != 1 {
		t.Fatalf("confirm: %+v %v", st, err)
	}
	if _, err := f.svc.ConfirmNetwork(ctx, uuid.Nil, id); !errors.Is(err, ErrNoPending) {
		t.Fatalf("confirm without pending: %v", err)
	}

	next := goodNet()
	next.IPv4.Address = "10.0.0.6/24"
	if _, err := f.svc.UpdateNetwork(ctx, uuid.Nil, id, NetworkInput{Network: next, ConfirmInterfaceChange: true}); err != nil {
		t.Fatal(err)
	}
	f.svc.now = func() time.Time { return now.Add(NetworkConfirmWindow + time.Second) }
	st, err = f.svc.Get(ctx, id)
	if err != nil || st.Pending != nil {
		t.Fatalf("auto-revert: %+v %v", st, err)
	}
	if len(f.repo.pending) != 0 {
		t.Fatal("expired pending change not cleared")
	}

	f.svc.now = func() time.Time { return now }
	if _, err := f.svc.UpdateNetwork(ctx, uuid.Nil, id, NetworkInput{Network: next, ConfirmInterfaceChange: true}); err != nil {
		t.Fatal(err)
	}
	if st, err = f.svc.RevertNetwork(ctx, uuid.Nil, id); err != nil || host.reverts != 1 || st.Pending != nil {
		t.Fatalf("revert: %+v %v", st, err)
	}
	if len(f.repo.audits) == 0 {
		t.Fatal("no audit saves")
	}
}

func TestNodeNetworkDNSOnlyOnStaticHost(t *testing.T) {
	live := staticLive()
	host := &fakeHost{status: HostStatus{Network: live}}
	f := newNodeFixture(t, host, nil)

	req := live
	req.DNSServers = []string{"9.9.9.9"}
	if _, err := f.svc.UpdateNetwork(context.Background(), uuid.Nil, f.servers[0], NetworkInput{Network: req}); err != nil {
		t.Fatal(err)
	}
	if len(host.dnsOnlys) != 1 || !host.dnsOnlys[0] {
		t.Fatalf("dns-only change must take the minimal path: %v", host.dnsOnlys)
	}
}

func TestNodeNetworkRiskyChangeNeedsConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(Network) Network
	}{
		{"static-to-dhcp", func(n Network) Network { n.IPv4 = IPConfig{Mode: ModeDHCP}; return n }},
		{"moved-address", func(n Network) Network { n.IPv4.Address = "103.176.22.99/24"; return n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := staticLive()
			host := &fakeHost{status: HostStatus{Network: live}}
			f := newNodeFixture(t, host, nil)
			risky := tc.mutate(live)
			if _, err := f.svc.UpdateNetwork(context.Background(), uuid.Nil, f.servers[0], NetworkInput{Network: risky}); !errors.Is(err, ErrRiskyNetwork) {
				t.Fatalf("risky change without confirmation: %v", err)
			}
			if host.applied != 0 {
				t.Fatal("refused change reached the host")
			}
			if _, err := f.svc.UpdateNetwork(context.Background(), uuid.Nil, f.servers[0], NetworkInput{Network: risky, ConfirmInterfaceChange: true}); err != nil {
				t.Fatalf("confirmed change rejected: %v", err)
			}
			if len(host.dnsOnlys) != 1 || host.dnsOnlys[0] {
				t.Fatalf("interface change must take the full path: %v", host.dnsOnlys)
			}
		})
	}
}

func sshStatus() SSHSettings {
	return SSHSettings{
		Available: true, Port: 22, PasswordAuthentication: true, KeyAuthAvailable: true,
	}
}

func TestNodeSSHPortChangeVerifiesAndSaves(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint32(listener.Addr().(*net.TCPAddr).Port)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		_, _ = conn.Write([]byte("SSH-2.0-test\r\n"))
		_ = conn.Close()
	}()

	host := &fakeHost{status: HostStatus{SSH: sshStatus()}}
	f := newNodeFixture(t, host, nil)
	f.repo.sshUser = "deploy"
	st, err := f.svc.UpdateSSH(context.Background(), uuid.Nil, f.servers[0], SSHInput{
		Port: port, PasswordAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(host.sshApplied) != 1 || host.sshApplied[0].Port != port {
		t.Fatalf("apply = %+v", host.sshApplied)
	}
	if len(host.sshUsers) != 1 || host.sshUsers[0] != "deploy" {
		t.Fatalf("apply user = %v, want the server record's SSH user", host.sshUsers)
	}
	if host.sshConfirmed != 1 || host.sshReverted != 0 {
		t.Fatalf("confirm=%d revert=%d", host.sshConfirmed, host.sshReverted)
	}
	if f.repo.port != int(port) {
		t.Fatalf("saved port = %d, want %d", f.repo.port, port)
	}
	if st.Capability != CapabilityReady {
		t.Fatalf("state = %+v", st)
	}
}

func TestNodeSSHPortChangeRollsBackWhenDialFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint32(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	host := &fakeHost{status: HostStatus{SSH: sshStatus()}}
	f := newNodeFixture(t, host, nil)
	_, err = f.svc.UpdateSSH(context.Background(), uuid.Nil, f.servers[0], SSHInput{
		Port: port, PasswordAuthentication: true,
	})
	var fields FieldErrors
	if !errors.As(err, &fields) || fields["port"] == "" {
		t.Fatalf("err = %v, want a port field error", err)
	}
	if host.sshConfirmed != 0 || host.sshReverted != 1 {
		t.Fatalf("confirm=%d revert=%d, want a revert", host.sshConfirmed, host.sshReverted)
	}
	if len(f.repo.ports) != 0 {
		t.Fatalf("a failed port change reached the server record: %v", f.repo.ports)
	}
}

func TestNodeSSHConfirmFailureRollsBackRecord(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint32(listener.Addr().(*net.TCPAddr).Port)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		_, _ = conn.Write([]byte("SSH-2.0-test\r\n"))
		_ = conn.Close()
	}()

	host := &fakeHost{status: HostStatus{SSH: sshStatus()}, sshConfirmErr: errors.New("confirm failed")}
	f := newNodeFixture(t, host, nil)
	if _, err := f.svc.UpdateSSH(context.Background(), uuid.Nil, f.servers[0], SSHInput{
		Port: port, PasswordAuthentication: true,
	}); err == nil {
		t.Fatal("confirm failure must fail the change")
	}
	if host.sshReverted != 1 {
		t.Fatalf("reverts = %d, want 1", host.sshReverted)
	}
	if len(f.repo.ports) != 2 || f.repo.ports[0] != int(port) || f.repo.ports[1] != 22 {
		t.Fatalf("record rollback = %v, want [%d 22]", f.repo.ports, port)
	}
}

func TestNodeSSHRevertUsesDetachedContext(t *testing.T) {
	host := &fakeHost{}
	f := newNodeFixture(t, host, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.svc.revertSSH(ctx, NodeHost{Applier: host}, f.servers[0], SSHSettings{Port: 22}, true)
	if host.sshReverted != 1 {
		t.Fatalf("a cancelled request must still run the host revert: %d", host.sshReverted)
	}
	if len(f.repo.ports) != 1 || f.repo.ports[0] != 22 {
		t.Fatalf("record rollback = %v, want [22]", f.repo.ports)
	}
}

func TestNodeSSHDisablePasswordNeedsKeyAuth(t *testing.T) {
	status := sshStatus()
	status.KeyAuthAvailable = false
	host := &fakeHost{status: HostStatus{SSH: status}}
	f := newNodeFixture(t, host, nil)
	_, err := f.svc.UpdateSSH(context.Background(), uuid.Nil, f.servers[0], SSHInput{
		Port: 22, PasswordAuthentication: false,
	})
	var fields FieldErrors
	if !errors.As(err, &fields) || fields["password_authentication"] == "" {
		t.Fatalf("err = %v, want a password-authentication field error", err)
	}
	if len(host.sshApplied) != 0 {
		t.Fatal("a blocked change reached the host")
	}
}

func TestNodeSSHPasswordOnlyChangeNeedsNoTrial(t *testing.T) {
	host := &fakeHost{status: HostStatus{SSH: sshStatus()}}
	f := newNodeFixture(t, host, nil)
	if _, err := f.svc.UpdateSSH(context.Background(), uuid.Nil, f.servers[0], SSHInput{
		Port: 22, PasswordAuthentication: false,
	}); err != nil {
		t.Fatal(err)
	}
	if len(host.sshApplied) != 1 || host.sshConfirmed != 0 {
		t.Fatalf("apply=%+v confirm=%d", host.sshApplied, host.sshConfirmed)
	}
}

func TestNodeAgentRootWaitsForReconnect(t *testing.T) {
	host := &fakeHost{status: HostStatus{AgentRoot: false}}
	f := newNodeFixture(t, host, nil)
	if _, err := f.svc.UpdateAgentRoot(context.Background(), uuid.Nil, f.servers[0], true); err != nil {
		t.Fatal(err)
	}
	if len(host.agentApplied) != 1 || !host.agentApplied[0] {
		t.Fatalf("agent apply = %+v", host.agentApplied)
	}
	if len(f.repo.audits) == 0 || f.repo.audits[0] != NodeSectionAgent {
		t.Fatalf("audits = %v", f.repo.audits)
	}
}

func TestNodeAgentRootReportsRestarting(t *testing.T) {
	host := &fakeHost{status: HostStatus{AgentRoot: false}, agentStays: true}
	f := newNodeFixture(t, host, nil)
	f.svc.agentWaitTimeout = 0
	f.svc.agentWaitPoll = time.Millisecond
	if _, err := f.svc.UpdateAgentRoot(context.Background(), uuid.Nil, f.servers[0], true); !errors.Is(err, ErrAgentRestarting) {
		t.Fatalf("err = %v, want ErrAgentRestarting", err)
	}
}

func TestNodePendingChangeIsPerNode(t *testing.T) {
	hostA := &fakeHost{status: HostStatus{Network: goodNet()}}
	hostB := &fakeHost{status: HostStatus{Network: goodNet()}}
	f := newNodeFixture(t, hostA, nil)
	idB := uuid.New()
	f.nodes.hosts[idB] = NodeHost{Applier: hostB}

	ctx := context.Background()
	if _, err := f.svc.UpdateNetwork(ctx, uuid.Nil, f.servers[0], NetworkInput{Network: goodNet()}); err != nil {
		t.Fatal(err)
	}
	st, err := f.svc.Get(ctx, idB)
	if err != nil || st.Pending != nil {
		t.Fatalf("node B saw node A's pending change: %+v %v", st.Pending, err)
	}
	if _, err := f.svc.UpdateNetwork(ctx, uuid.Nil, idB, NetworkInput{Network: goodNet()}); err != nil {
		t.Fatalf("node B could not start its own change: %v", err)
	}
	if len(f.repo.pending) != 2 {
		t.Fatalf("want two pending changes, got %d", len(f.repo.pending))
	}
	if _, err := f.svc.ConfirmNetwork(ctx, uuid.Nil, f.servers[0]); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.repo.pending[idB]; !ok {
		t.Fatal("confirming node A cleared node B's pending change")
	}
}

func TestNodeUnsupportedAndOutdated(t *testing.T) {
	ctx := context.Background()
	unsupported := &fakeHost{status: HostStatus{Capability: CapabilityUnsupportedStack}}
	f := newNodeFixture(t, unsupported, nil)
	if _, err := f.svc.UpdateSystem(ctx, uuid.Nil, f.servers[0], System{Hostname: "gotham-1", NTPEnabled: true}); !isUnsupported(err, CapabilityUnsupportedStack) {
		t.Fatalf("want unsupported capability, got %v", err)
	}
	st, err := f.svc.Get(ctx, f.servers[0])
	if err != nil || st.Capability != CapabilityUnsupportedStack || st.Reason == "" {
		t.Fatalf("unsupported node state: %+v %v", st, err)
	}

	outdated := &fakeHost{status: HostStatus{Capability: CapabilityHelperOutdated, Repair: true}}
	f = newNodeFixture(t, outdated, nil)
	if _, err := f.svc.UpdateSystem(ctx, uuid.Nil, f.servers[0], System{Hostname: "gotham-1", NTPEnabled: true}); !isUnsupported(err, CapabilityHelperOutdated) {
		t.Fatalf("want helper_outdated capability, got %v", err)
	}
	st, err = f.svc.Get(ctx, f.servers[0])
	if err != nil || st.Capability != CapabilityHelperOutdated || st.Reason == "" {
		t.Fatalf("outdated helper state: %+v %v", st, err)
	}

	for _, tc := range []struct {
		err  error
		want NodeCapability
	}{
		{ErrAgentOutdated, CapabilityAgentOutdated},
		{ErrNodeOffline, CapabilityOffline},
	} {
		host := &fakeHost{statusErr: tc.err}
		f := newNodeFixture(t, host, nil)
		st, err := f.svc.Get(ctx, f.servers[0])
		if err != nil || st.Capability != tc.want || st.Reason == "" {
			t.Fatalf("%s: state=%+v err=%v", tc.want, st, err)
		}
		if _, err := f.svc.UpdateNetwork(ctx, uuid.Nil, f.servers[0], NetworkInput{Network: goodNet()}); !isUnsupported(err, tc.want) {
			t.Fatalf("%s: change must be refused with the capability reason, got %v", tc.want, err)
		}
	}
}

func isUnsupported(err error, want NodeCapability) bool {
	var u *UnsupportedError
	return errors.As(err, &u) && u.Capability == want
}

func TestNodeInstallHelper(t *testing.T) {
	ctx := context.Background()
	missing := &fakeHost{status: HostStatus{Capability: CapabilityHelperMissing, Repair: true}, installLog: "installed gotham-hostctl\n"}
	f := newNodeFixture(t, missing, nil)
	f.nodes.hosts[f.servers[0]] = NodeHost{Applier: missing, Local: true}
	if _, log, err := f.svc.InstallHelper(ctx, uuid.Nil, f.servers[0]); err != nil || missing.installs != 1 || log == "" {
		t.Fatalf("install: %q %v", log, err)
	}

	denied := &fakeHost{status: HostStatus{Capability: CapabilityHelperDenied, Repair: false}}
	f = newNodeFixture(t, denied, nil)
	f.nodes.hosts[f.servers[0]] = NodeHost{Applier: denied, Local: true}
	if _, _, err := f.svc.InstallHelper(ctx, uuid.Nil, f.servers[0]); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("denied install = %v, want ErrUnsupported", err)
	}
	if denied.installs != 0 {
		t.Fatalf("denied host must not attempt an install: %d", denied.installs)
	}

	remote := &fakeHost{status: HostStatus{Capability: CapabilityHelperMissing, Repair: true}}
	f = newNodeFixture(t, remote, nil)
	if _, _, err := f.svc.InstallHelper(ctx, uuid.Nil, f.servers[0]); !isUnsupported(err, CapabilityHelperMissing) {
		t.Fatalf("remote install = %v, want capability refusal", err)
	}
	if remote.installs != 0 {
		t.Fatalf("remote host must not attempt an install: %d", remote.installs)
	}
}

func TestNodeLocalTimezone(t *testing.T) {
	ctx := context.Background()
	general := NewService(&fakeRepo{}, quietLogger())
	general.lookup = func(string) string { return "" }
	host := &fakeHost{status: HostStatus{}}
	f := newNodeFixture(t, host, general)
	id := f.servers[0]
	f.nodes.hosts[id] = NodeHost{Applier: host, Local: true}

	st, err := f.svc.Get(ctx, id)
	if err != nil || st.System.Timezone != DefaultTimezone {
		t.Fatalf("local timezone fallback: %q %v", st.System.Timezone, err)
	}
	if _, err := f.svc.UpdateSystem(ctx, uuid.Nil, id, System{Hostname: "gotham-1", NTPEnabled: true, Timezone: "Europe/Berlin"}); err != nil {
		t.Fatal(err)
	}
	if host.lastSystem.Timezone != "Europe/Berlin" {
		t.Fatalf("timezone not applied to the host: %+v", host.lastSystem)
	}
	if tz, _ := general.Timezone(ctx); tz != "Europe/Berlin" {
		t.Fatalf("local timezone not stored for schedules: %q", tz)
	}

	general.lookup = func(k string) string {
		if k == EnvTimezone {
			return "Asia/Tokyo"
		}
		return ""
	}
	_, err = f.svc.UpdateSystem(ctx, uuid.Nil, id, System{NTPEnabled: true, Timezone: "Europe/Berlin"})
	var fe FieldErrors
	if !errors.As(err, &fe) || fe["timezone"] == "" {
		t.Fatalf("env-locked timezone change not refused: %v", err)
	}
}

func TestNodeSystemValidationAndApplyError(t *testing.T) {
	ctx := context.Background()
	host := &fakeHost{status: HostStatus{}}
	f := newNodeFixture(t, host, nil)
	if _, err := f.svc.UpdateSystem(ctx, uuid.Nil, f.servers[0], System{Hostname: "-bad-", NTPEnabled: true}); err == nil {
		t.Fatal("invalid hostname accepted")
	}
	host.applyErr = ErrHost
	if _, err := f.svc.UpdateSystem(ctx, uuid.Nil, f.servers[0], System{Hostname: "gotham-1", NTPEnabled: true}); !errors.Is(err, ErrHost) {
		t.Fatalf("want host error: %v", err)
	}
}

func TestNodeWithoutDialableAddressIsOffline(t *testing.T) {
	ctx := context.Background()
	f := newNodeFixture(t, &fakeHost{}, nil)
	id := f.servers[0]
	f.nodes.hosts[id] = NodeHost{Applier: NewAgentApplier(func(context.Context) (agentv1.HostServiceClient, func(), error) {
		return nil, nil, errors.New("servers: validation failed: server has no dialable address")
	})}

	st, err := f.svc.Get(ctx, id)
	if err != nil || st.Capability != CapabilityOffline || st.Reason == "" {
		t.Fatalf("state: %+v %v", st, err)
	}

	ok := chi.NewRouter()
	MountNode(ok, func(n http.Handler) http.Handler { return n }, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }, f.svc)
	rec := httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/servers/"+id.String()+"/host", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"capability":"offline"`) {
		t.Fatalf("read-only settings: %d %s", rec.Code, rec.Body)
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodPut, "/host/system", `{"hostname":"node-a","ntp_enabled":true}`},
		{http.MethodPut, "/host/network", `{"ipv4":{"mode":"dhcp"},"ipv6":{"enabled":false,"mode":"dhcp"}}`},
		{http.MethodPut, "/host/ssh", `{"port":2222,"password_authentication":true}`},
		{http.MethodPut, "/host/agent-root", `{"enabled":true}`},
		{http.MethodPost, "/host/helper", ""},
	} {
		rec = httptest.NewRecorder()
		ok.ServeHTTP(rec, httptest.NewRequest(tc.method, "/v1/servers/"+id.String()+tc.path, strings.NewReader(tc.body)))
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s %s = %d, want 409: %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestNodeRoutes(t *testing.T) {
	host := &fakeHost{status: HostStatus{Network: goodNet()}}
	f := newNodeFixture(t, host, nil)
	id := f.servers[0]

	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	}
	r := chi.NewRouter()
	MountNode(r, deny, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }, f.svc)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/servers/"+id.String()+"/host", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner got %d", rec.Code)
	}

	ok := chi.NewRouter()
	MountNode(ok, func(n http.Handler) http.Handler { return n }, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }, f.svc)
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/servers/"+id.String()+"/host", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"capability":"ready"`) {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/servers/not-a-uuid/host", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/servers/"+uuid.New().String()+"/host", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/servers/"+id.String()+"/host/network",
		bytes.NewBufferString(`{"dns_servers":["nope"],"ipv4":{"mode":"dhcp"},"ipv6":{"enabled":false,"mode":"dhcp"}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid network: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/servers/"+id.String()+"/host/network/confirm", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("confirm without pending: %d %s", rec.Code, rec.Body)
	}
}

func TestNodeAgentRootRestartingIsConflict(t *testing.T) {
	host := &fakeHost{status: HostStatus{AgentRoot: false}, agentStays: true}
	f := newNodeFixture(t, host, nil)
	f.svc.agentWaitTimeout = 0
	f.svc.agentWaitPoll = time.Millisecond
	id := f.servers[0]
	r := chi.NewRouter()
	MountNode(r, func(n http.Handler) http.Handler { return n },
		func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }, f.svc)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/servers/"+id.String()+"/host/agent-root",
		strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "restarting") {
		t.Fatalf("restarting = %d %s", rec.Code, rec.Body)
	}
}

func TestNodeSystemHelperFailureShowsReason(t *testing.T) {
	host := &fakeHost{status: HostStatus{}}
	f := newNodeFixture(t, host, nil)
	id := f.servers[0]
	host.applyErr = fmt.Errorf("%w: apply-system: mkdir: cannot create directory '/etc/systemd/timesyncd.conf.d': Read-only file system", ErrHost)

	r := chi.NewRouter()
	MountNode(r, func(n http.Handler) http.Handler { return n }, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }, f.svc)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/servers/"+id.String()+"/host/system",
		bytes.NewBufferString(`{"hostname":"","ntp_enabled":true,"ntp_servers":[],"timezone":"Europe/Berlin"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("helper failure = %d, want 502: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"message":"the host helper failed; the change was not applied"`,
		`"log":"apply-system: mkdir: cannot create directory '/etc/systemd/timesyncd.conf.d': Read-only file system"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("502 body misses %s:\n%s", want, body)
		}
	}
}

func TestNodeSystemAgentHelperFailureShowsReason(t *testing.T) {
	host := &fakeHost{status: HostStatus{}}
	f := newNodeFixture(t, host, nil)
	id := f.servers[0]
	host.applyErr = errors.Join(ErrHost, status.Error(codes.Internal, "the host helper failed: gotham-hostctl: cannot prepare directories"))

	r := chi.NewRouter()
	MountNode(r, func(n http.Handler) http.Handler { return n }, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }, f.svc)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/servers/"+id.String()+"/host/system",
		bytes.NewBufferString(`{"hostname":"","ntp_enabled":true,"ntp_servers":[],"timezone":"Europe/Berlin"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("agent helper failure = %d, want 502: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"log":"the host helper failed: gotham-hostctl: cannot prepare directories"`) {
		t.Errorf("502 body misses the agent's helper reason:\n%s", body)
	}
	if strings.Contains(body, "rpc error") {
		t.Errorf("502 body leaks gRPC transport text:\n%s", body)
	}
}

func TestNodeConcurrentApplyOnOneNode(t *testing.T) {
	host := &fakeHost{status: HostStatus{Network: goodNet()}}
	f := newNodeFixture(t, host, nil)
	id := f.servers[0]

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.UpdateNetwork(context.Background(), uuid.Nil, id, NetworkInput{Network: goodNet(), ConfirmInterfaceChange: true})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	var refused int
	for err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, ErrPending):
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if refused != 1 || host.applied != 1 {
		t.Fatalf("want exactly one apply on the node, refused=%d applied=%d", refused, host.applied)
	}
}
