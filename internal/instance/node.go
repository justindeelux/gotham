package instance

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// NodeHost is one server's host-control handle. Local means the node runs on
// the control plane's own machine: only there can the helper be repaired, and
// only there is the timezone also the control plane's own.
type NodeHost struct {
	Applier HostApplier
	Local   bool
}

type NodeResolver interface {
	NodeHost(ctx context.Context, serverID uuid.UUID) (NodeHost, error)
}

// UnsupportedError is a node whose host settings cannot be changed, with the
// capability that explains why. It is a conflict, never a server error.
type UnsupportedError struct {
	Capability NodeCapability
	Message    string
}

func (e *UnsupportedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return capabilityReason(e.Capability)
}

// NodeService reads and changes the host settings of one server at a time.
// Nothing is cached: every read asks that node's host.
type NodeService struct {
	repo    NodeRepository
	nodes   NodeResolver
	general *Service
	logger  *slog.Logger
	now     func() time.Time
	// One lock per node: an offline node must not block another node's
	// request, but two changes on the same node must not race the pending
	// check (the loser would revert the winner's host change).
	locks sync.Map
	// Root mode restarts the agent; poll that long for it to come back.
	agentWaitTimeout time.Duration
	agentWaitPoll    time.Duration
}

func NewNodeService(repo NodeRepository, nodes NodeResolver, general *Service, logger *slog.Logger) *NodeService {
	if logger == nil {
		logger = slog.Default()
	}
	return &NodeService{
		repo: repo, nodes: nodes, general: general, logger: logger, now: time.Now,
		agentWaitTimeout: 45 * time.Second,
		agentWaitPoll:    time.Second,
	}
}

func capabilityReason(c NodeCapability) string {
	switch c {
	case CapabilityUnsupportedStack:
		return "this server does not run the systemd network and time stack Gotham needs"
	case CapabilityHelperOutdated:
		return "this server's host helper is too old for host settings; update it to change them from here"
	case CapabilityHelperMissing:
		return "the host helper is not installed on this server"
	case CapabilityHelperDenied:
		return "this server may not run the host helper"
	case CapabilityAgentOutdated:
		return "this server's agent is too old for host settings; update the agent to change them from here"
	case CapabilityOffline:
		return "this server's agent is not reachable"
	}
	return ""
}

func (s *NodeService) Get(ctx context.Context, serverID uuid.UUID) (NodeState, error) {
	return s.state(ctx, serverID)
}

func (s *NodeService) lock(serverID uuid.UUID) func() {
	value, _ := s.locks.LoadOrStore(serverID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *NodeService) UpdateSystem(ctx context.Context, actor, serverID uuid.UUID, in System) (NodeState, error) {
	unlock := s.lock(serverID)
	defer unlock()
	in, errs := normalizeSystem(in)
	if errs != nil {
		return NodeState{}, errs
	}
	host, _, err := s.ready(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	if host.Local && s.general != nil {
		cur, err := s.general.TimezoneField(ctx)
		if err != nil {
			return NodeState{}, err
		}
		if in.Timezone == "" {
			in.Timezone = cur.Value
		}
		if cur.Locked && in.Timezone != cur.Value {
			return NodeState{}, FieldErrors{"timezone": "locked by environment variable " + cur.EnvVar}
		}
	}
	if err := host.Applier.ApplySystem(ctx, in); err != nil {
		return NodeState{}, err
	}
	if host.Local && s.general != nil {
		if err := s.general.SetTimezone(ctx, actor, in.Timezone); err != nil {
			return NodeState{}, err
		}
	}
	s.audit(ctx, actor, NodeSectionSystem, serverID, map[string]any{
		"hostname": in.Hostname, "ntp_enabled": in.NTPEnabled, "ntp_servers": in.NTPServers, "timezone": in.Timezone,
	})
	s.logger.Info("node host system settings updated", "actor", actor, "server_id", serverID)
	return s.state(ctx, serverID)
}

func (s *NodeService) UpdateNetwork(ctx context.Context, actor, serverID uuid.UUID, in NetworkInput) (NodeState, error) {
	unlock := s.lock(serverID)
	defer unlock()
	net, errs := normalizeNetwork(in.Network)
	if errs != nil {
		return NodeState{}, errs
	}
	host, status, err := s.ready(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	pending, err := s.reconcilePending(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	if pending != nil {
		return NodeState{}, ErrPending
	}
	dnsOnly := false
	switch {
	case sameNetwork(status.Network, net):
		dnsOnly = true
	case riskyNetworkChange(status.Network, net) && !in.ConfirmInterfaceChange:
		return NodeState{}, ErrRiskyNetwork
	}
	if err := host.Applier.ApplyNetwork(ctx, net, NetworkConfirmWindow, dnsOnly); err != nil {
		return NodeState{}, err
	}
	next := Pending{Previous: status.Network, Proposed: net, Deadline: s.now().Add(NetworkConfirmWindow)}
	ok, err := s.repo.BeginPending(ctx, serverID, next)
	if err != nil || !ok {
		if rerr := host.Applier.RevertNetwork(ctx); rerr != nil {
			s.logger.Error("node network revert after failed pending save", "server_id", serverID, "error", rerr)
		}
		if err == nil {
			err = ErrPending
		}
		return NodeState{}, err
	}
	s.audit(ctx, actor, NodeSectionNetwork, serverID, map[string]any{"event": "applied, awaiting confirmation", "proposed": net})
	s.logger.Info("node network change applied, awaiting confirmation", "actor", actor, "server_id", serverID)
	return s.state(ctx, serverID)
}

// UpdateSSH changes the SSH port and password login. A port change is verified
// with a new TCP connection before it is saved on the server record and
// confirmed on the host; any failure restores the previous port, which stayed
// open during the trial.
func (s *NodeService) UpdateSSH(ctx context.Context, actor, serverID uuid.UUID, in SSHInput) (NodeState, error) {
	unlock := s.lock(serverID)
	defer unlock()
	if in.Port < 1 || in.Port > 65535 {
		return NodeState{}, FieldErrors{"port": "port must be between 1 and 65535"}
	}
	host, status, err := s.ready(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	if !status.SSH.Available {
		return NodeState{}, FieldErrors{"port": "sshd is not installed on this server"}
	}
	if status.SSH.PasswordAuthentication && !in.PasswordAuthentication && !status.SSH.KeyAuthAvailable {
		return NodeState{}, FieldErrors{
			"password_authentication": "cannot disable password login: no authorized key is configured on this server",
		}
	}
	portChanged := in.Port != status.SSH.Port
	passwordChanged := in.PasswordAuthentication != status.SSH.PasswordAuthentication
	if !portChanged && !passwordChanged {
		return s.state(ctx, serverID)
	}
	if err := host.Applier.ApplySSH(ctx, in); err != nil {
		return NodeState{}, err
	}
	previous := status.SSH
	if portChanged {
		ip, err := s.repo.ServerAddress(ctx, serverID)
		if err != nil {
			s.revertSSH(ctx, host, serverID, previous, false)
			return NodeState{}, err
		}
		if err := verifySSHPort(ctx, ip, int(in.Port)); err != nil {
			s.revertSSH(ctx, host, serverID, previous, false)
			return NodeState{}, FieldErrors{
				"port": fmt.Sprintf(
					"the server did not accept a connection on port %d; the previous port was restored", in.Port,
				),
			}
		}
		saved := !host.Local
		if saved {
			if err := s.repo.SetSSHPort(ctx, serverID, int(in.Port)); err != nil {
				s.revertSSH(ctx, host, serverID, previous, false)
				return NodeState{}, err
			}
		}
		if err := host.Applier.ConfirmSSH(ctx); err != nil {
			s.revertSSH(ctx, host, serverID, previous, saved)
			return NodeState{}, err
		}
	}
	s.audit(ctx, actor, NodeSectionSSH, serverID, map[string]any{
		"port": in.Port, "password_authentication": in.PasswordAuthentication,
	})
	s.logger.Info("node SSH settings updated", "actor", actor, "server_id", serverID, "port", in.Port)
	return s.state(ctx, serverID)
}

// revertSSH restores the previous SSH state and, when it was already saved,
// the previous port on the server record.
func (s *NodeService) revertSSH(ctx context.Context, host NodeHost, serverID uuid.UUID, previous SSHSettings, saved bool) {
	if err := host.Applier.RevertSSH(ctx); err != nil {
		s.logger.Error("node SSH revert failed", "server_id", serverID, "error", err)
	}
	if saved {
		if err := s.repo.SetSSHPort(ctx, serverID, int(previous.Port)); err != nil {
			s.logger.Error("node SSH port rollback failed", "server_id", serverID, "error", err)
		}
	}
}

// UpdateAgentRoot switches the agent's service user and waits for it to
// reconnect. The helper schedules the restart, so the apply call returns
// before the connection drops.
func (s *NodeService) UpdateAgentRoot(ctx context.Context, actor, serverID uuid.UUID, enabled bool) (NodeState, error) {
	unlock := s.lock(serverID)
	defer unlock()
	host, status, err := s.ready(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	if status.AgentRoot == enabled {
		return s.state(ctx, serverID)
	}
	if err := host.Applier.ApplyAgentRoot(ctx, enabled); err != nil {
		// The restart can drop the connection before the reply arrives; the
		// poll below is the authority on whether the change took effect.
		if !errors.Is(err, ErrNodeOffline) {
			return NodeState{}, err
		}
	}
	deadline := s.now().Add(s.agentWaitTimeout)
	for {
		current, err := s.status(ctx, host)
		if err == nil && current.Capability == CapabilityReady && current.AgentRoot == enabled {
			s.audit(ctx, actor, NodeSectionAgent, serverID, map[string]any{"agent_root": enabled})
			s.logger.Info("node agent root mode updated", "actor", actor, "server_id", serverID, "enabled", enabled)
			return s.state(ctx, serverID)
		}
		if s.now().After(deadline) {
			return NodeState{}, ErrAgentRestarting
		}
		select {
		case <-ctx.Done():
			return NodeState{}, ctx.Err()
		case <-time.After(s.agentWaitPoll):
		}
	}
}

// verifySSHPort opens a new connection on the new port and requires an SSH
// banner, so a random service on that port cannot pass for sshd.
func verifySSHPort(ctx context.Context, host string, port int) error {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	banner, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(banner, "SSH-") {
		return fmt.Errorf("unexpected SSH banner on port %d", port)
	}
	return nil
}

func (s *NodeService) ConfirmNetwork(ctx context.Context, actor, serverID uuid.UUID) (NodeState, error) {
	return s.settle(ctx, actor, serverID, true)
}

func (s *NodeService) RevertNetwork(ctx context.Context, actor, serverID uuid.UUID) (NodeState, error) {
	return s.settle(ctx, actor, serverID, false)
}

func (s *NodeService) settle(ctx context.Context, actor, serverID uuid.UUID, confirm bool) (NodeState, error) {
	unlock := s.lock(serverID)
	defer unlock()
	host, _, err := s.ready(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	pending, err := s.reconcilePending(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	if pending == nil {
		return NodeState{}, ErrNoPending
	}
	event := "confirmed"
	if confirm {
		err = host.Applier.ConfirmNetwork(ctx)
	} else {
		event = "reverted by operator"
		err = host.Applier.RevertNetwork(ctx)
	}
	if err != nil {
		return NodeState{}, err
	}
	if err := s.repo.ClearPending(ctx, serverID); err != nil {
		return NodeState{}, err
	}
	s.audit(ctx, actor, NodeSectionNetwork, serverID, map[string]any{"event": event})
	s.logger.Info("node network change settled", "actor", actor, "server_id", serverID, "event", event)
	return s.state(ctx, serverID)
}

// InstallHelper repairs the helper on the control plane's own node. A remote
// node is repaired by re-running the agent installer, so it reports why the
// action is refused instead of pretending to try.
func (s *NodeService) InstallHelper(ctx context.Context, actor, serverID uuid.UUID) (NodeState, string, error) {
	unlock := s.lock(serverID)
	defer unlock()
	host, err := s.node(ctx, serverID)
	if err != nil {
		return NodeState{}, "", err
	}
	status, err := s.status(ctx, host)
	if err != nil {
		return NodeState{}, "", err
	}
	if !host.Local {
		return NodeState{}, "", &UnsupportedError{
			Capability: status.Capability,
			Message:    "only the server running the control plane can repair the host helper here; update this server's agent instead",
		}
	}
	if status.Capability == CapabilityReady {
		state, err := s.state(ctx, serverID)
		return state, "", err
	}
	if !status.Repair {
		return NodeState{}, "", ErrUnsupported
	}
	log, err := host.Applier.InstallHostHelper(ctx)
	s.logger.Info("node host helper install",
		"actor", actor, "server_id", serverID, "ok", err == nil, "capability_before", status.Capability)
	if err != nil {
		return NodeState{}, log, err
	}
	state, err := s.state(ctx, serverID)
	return state, log, err
}

func (s *NodeService) state(ctx context.Context, serverID uuid.UUID) (NodeState, error) {
	host, err := s.node(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	pending, err := s.reconcilePending(ctx, serverID)
	if err != nil {
		return NodeState{}, err
	}
	status, err := s.status(ctx, host)
	if err != nil {
		return NodeState{}, err
	}
	reason := status.Reason
	if reason == "" {
		reason = capabilityReason(status.Capability)
	}
	state := NodeState{
		Capability: status.Capability,
		Reason:     reason,
		Repair:     status.Repair,
		Network:    status.Network,
		System:     status.System,
		SSH:        status.SSH,
		AgentRoot:  status.AgentRoot,
		Pending:    pending,
	}
	if pending != nil {
		state.Network = pending.Proposed
	}
	if host.Local && s.general != nil {
		// The control plane's own zone is the same machine's zone: use the
		// stored value when the host could not report it, and an environment
		// override so the form shows the zone schedules actually run in.
		if cur, err := s.general.TimezoneField(ctx); err == nil && (state.System.Timezone == "" || cur.Locked) {
			state.System.Timezone = cur.Value
		}
	}
	return state, nil
}

func (s *NodeService) node(ctx context.Context, serverID uuid.UUID) (NodeHost, error) {
	host, err := s.nodes.NodeHost(ctx, serverID)
	if err != nil {
		return NodeHost{}, err
	}
	if host.Applier == nil {
		return NodeHost{}, ErrNodeNotFound
	}
	return host, nil
}

// ready reports the node only when it can apply settings; anything else is a
// conflict with the capability that explains it.
func (s *NodeService) ready(ctx context.Context, serverID uuid.UUID) (NodeHost, HostStatus, error) {
	host, err := s.node(ctx, serverID)
	if err != nil {
		return NodeHost{}, HostStatus{}, err
	}
	status, err := s.status(ctx, host)
	if err != nil {
		return NodeHost{}, HostStatus{}, err
	}
	if status.Capability != CapabilityReady {
		return host, status, &UnsupportedError{Capability: status.Capability}
	}
	return host, status, nil
}

// status folds a transport failure into the capability the UI shows, so an
// outdated or offline agent is read-only data instead of a server error.
func (s *NodeService) status(ctx context.Context, host NodeHost) (HostStatus, error) {
	status, err := host.Applier.Status(ctx)
	switch {
	case err == nil:
		return status, nil
	case errors.Is(err, ErrAgentOutdated):
		return HostStatus{Capability: CapabilityAgentOutdated}, nil
	case errors.Is(err, ErrNodeOffline):
		return HostStatus{Capability: CapabilityOffline}, nil
	}
	return HostStatus{}, err
}

func (s *NodeService) reconcilePending(ctx context.Context, serverID uuid.UUID) (*Pending, error) {
	pending, err := s.repo.Pending(ctx, serverID)
	if err != nil || pending == nil {
		return pending, err
	}
	if s.now().Before(pending.Deadline) {
		return pending, nil
	}
	if err := s.repo.ClearPending(ctx, serverID); err != nil {
		return nil, err
	}
	// The host's own timer already restored the previous state.
	s.logger.Warn("node network change auto-reverted: not confirmed in time", "server_id", serverID)
	return nil, nil
}

func (s *NodeService) audit(ctx context.Context, actor uuid.UUID, section string, serverID uuid.UUID, changes map[string]any) {
	changes["server_id"] = serverID
	if err := s.repo.Audit(ctx, actor, section, changes); err != nil {
		s.logger.Error("node host settings audit failed", "server_id", serverID, "section", section, "error", err)
	}
}

func sameNetwork(live, req Network) bool {
	return live.IPv4 == req.IPv4 && live.IPv6 == req.IPv6
}

func riskyNetworkChange(live, req Network) bool {
	if live.IPv4.Mode == ModeStatic && live.IPv4.Address != "" {
		if req.IPv4.Mode != ModeStatic || req.IPv4.Address != live.IPv4.Address ||
			req.IPv4.Gateway != live.IPv4.Gateway {
			return true
		}
	}
	if live.IPv6.Enabled && live.IPv6.Mode == ModeStatic && live.IPv6.Address != "" {
		if !req.IPv6.Enabled || req.IPv6.Mode != ModeStatic ||
			req.IPv6.Address != live.IPv6.Address || req.IPv6.Gateway != live.IPv6.Gateway {
			return true
		}
	}
	return false
}
