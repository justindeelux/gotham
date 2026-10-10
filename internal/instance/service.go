package instance

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Service reads and writes the instance settings.
type Service struct {
	repo    Repository
	host    HostApplier
	logger  *slog.Logger
	now     func() time.Time
	lookup  func(string) string
	netLock sync.Mutex // serializes network apply / confirm / revert / reconcile
}

// NewService builds a Service. host may be nil (no host sections).
func NewService(repo Repository, host HostApplier, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, host: host, logger: logger, now: time.Now, lookup: os.Getenv}
}

// resolve picks env > stored > default for one general field.
func (s *Service) resolve(env string, stored *string, def string) Field {
	if v := strings.TrimSpace(s.lookup(env)); v != "" {
		return Field{Value: v, Source: SourceEnv, Locked: true, EnvVar: env}
	}
	if stored != nil && *stored != "" {
		return Field{Value: *stored, Source: SourceDB}
	}
	return Field{Value: def, Source: SourceDefault}
}

func (s *Service) general(rec Stored) General {
	return General{
		ControlPlaneURL: s.resolve(EnvPublicURL, rec.ControlPlaneURL, ""),
		InstanceName:    s.resolve(EnvInstanceName, rec.InstanceName, DefaultInstanceName),
		Timezone:        s.resolve(EnvTimezone, rec.Timezone, DefaultTimezone),
	}
}

// ControlPlaneURL returns the effective public base URL ("" when unset); it is
// the accessor other packages use instead of reading gotham.yaml directly.
func (s *Service) ControlPlaneURL(ctx context.Context) (string, error) {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return "", err
	}
	return s.general(rec).ControlPlaneURL.Value, nil
}

func (s *Service) capabilities(ctx context.Context) Capabilities {
	if s.host == nil {
		return Capabilities{}
	}
	return s.host.Capabilities(ctx)
}

// Get returns the full state, first reconciling an expired network change.
// The network section reports the host's live configuration (the source of
// truth) whenever no change is pending and the helper can probe it, so the
// form never offers stale stored defaults such as DHCP on a static host.
func (s *Service) Get(ctx context.Context) (State, error) {
	rec, err := s.reconcile(ctx)
	if err != nil {
		return State{}, err
	}
	if rec.Pending == nil {
		if live, ok := s.hostLive(ctx); ok {
			rec.Network.IPv4, rec.Network.IPv6 = live.IPv4, live.IPv6
			// An empty live resolver list means unconfigured, not "no
			// DNS": keep the stored servers so the form stays submittable.
			if len(live.DNSServers) > 0 {
				rec.Network.DNSServers = live.DNSServers
			}
		}
	}
	return State{
		General: s.general(rec), Network: rec.Network, System: rec.System,
		Capabilities: s.capabilities(ctx), Pending: rec.Pending,
	}, nil
}

// hostLive returns the helper's live network configuration. It reports
// nothing when there is no host applier or the probe fails, in which case
// callers fall back to the stored values.
func (s *Service) hostLive(ctx context.Context) (Network, bool) {
	if s.host == nil {
		return Network{}, false
	}
	live, ok := s.host.HostNetwork(ctx)
	if !ok {
		return Network{}, false
	}
	net := Network{IPv4: live.IPv4, IPv6: live.IPv6, DNSServers: live.DNSServers}
	if net.DNSServers == nil {
		net.DNSServers = []string{}
	}
	return net, true
}

// sameInterface reports whether req leaves the interface configuration of
// live untouched (DNS excluded: a resolver-only change needs no interface
// rewrite and no risky-change confirmation).
func sameInterface(live HostNetwork, req Network) bool {
	return live.IPv4 == req.IPv4 && live.IPv6 == req.IPv6
}

// riskyInterfaceChange reports whether req would disturb the live addresses
// of the active interface: switching it from static to DHCP, disabling the
// family, or changing its address/gateway.
func riskyInterfaceChange(live HostNetwork, req Network) bool {
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

// Reconcile settles an expired pending network change; call it at startup.
func (s *Service) Reconcile(ctx context.Context) error {
	_, err := s.reconcile(ctx)
	return err
}

// reconcile: a pending change past its deadline was reverted by the host timer,
// so the desired state returns to the previous values.
func (s *Service) reconcile(ctx context.Context) (Stored, error) {
	s.netLock.Lock()
	defer s.netLock.Unlock()
	rec, err := s.repo.Load(ctx)
	if err != nil || rec.Pending == nil || s.now().Before(rec.Pending.Deadline) {
		return rec, err
	}
	rec.Network, rec.Pending = rec.Pending.Previous, nil
	if err := s.repo.Save(ctx, uuid.Nil, SectionNetwork, rec, map[string]any{"event": "auto-reverted: not confirmed in time"}); err != nil {
		return rec, err
	}
	s.logger.Warn("instance network change auto-reverted: not confirmed in time")
	return rec, nil
}

// UpdateGeneral validates and stores the general section.
func (s *Service) UpdateGeneral(ctx context.Context, actor uuid.UUID, in GeneralInput) (State, error) {
	in, errs := normalizeGeneral(in)
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	cur := s.general(rec)
	if errs == nil {
		errs = FieldErrors{}
	}
	for key, f := range map[string]struct {
		field Field
		value string
	}{
		"control_plane_url": {cur.ControlPlaneURL, in.ControlPlaneURL},
		"instance_name":     {cur.InstanceName, in.InstanceName},
		"timezone":          {cur.Timezone, in.Timezone},
	} {
		if f.field.Locked && f.field.Value != f.value {
			errs[key] = "locked by environment variable " + f.field.EnvVar
		}
	}
	if len(errs) > 0 {
		return State{}, errs
	}
	next := rec
	next.ControlPlaneURL = ptrOrNil(in.ControlPlaneURL, cur.ControlPlaneURL.Locked, rec.ControlPlaneURL)
	next.InstanceName = ptrOrNil(in.InstanceName, cur.InstanceName.Locked, rec.InstanceName)
	next.Timezone = ptrOrNil(in.Timezone, cur.Timezone.Locked, rec.Timezone)
	changes := diff(map[string]any{
		"control_plane_url": cur.ControlPlaneURL.Value, "instance_name": cur.InstanceName.Value, "timezone": cur.Timezone.Value,
	}, map[string]any{
		"control_plane_url": s.general(next).ControlPlaneURL.Value, "instance_name": s.general(next).InstanceName.Value, "timezone": s.general(next).Timezone.Value,
	})
	if err := s.repo.Save(ctx, actor, SectionGeneral, next, changes); err != nil {
		return State{}, err
	}
	s.logger.Info("instance general settings updated", "actor", actor, "changes", changes)
	return s.Get(ctx)
}

// ptrOrNil keeps the stored value for a locked field and stores the rest.
func ptrOrNil(v string, locked bool, stored *string) *string {
	if locked {
		return stored
	}
	if v == "" {
		return nil
	}
	return &v
}

// UpdateSystem validates, applies and stores the system section.
func (s *Service) UpdateSystem(ctx context.Context, actor uuid.UUID, in System) (State, error) {
	in, errs := normalizeSystem(in)
	if errs != nil {
		return State{}, errs
	}
	if !s.capabilities(ctx).System {
		return State{}, ErrUnsupported
	}
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	if err := s.host.ApplySystem(ctx, in); err != nil {
		return State{}, err
	}
	changes := diff(rec.System, in)
	rec.System = in
	if err := s.repo.Save(ctx, actor, SectionSystem, rec, changes); err != nil {
		return State{}, err
	}
	s.logger.Info("instance system settings updated", "actor", actor, "changes", changes)
	return s.Get(ctx)
}

// UpdateNetwork validates and tentatively applies a network change; it stays
// pending until ConfirmNetwork, and the host reverts it after the deadline.
// A resolver-only change on a static host is applied DNS-only (the interface
// file is never rewritten); a change that would disturb the active
// interface's addresses needs ConfirmInterfaceChange and is refused without
// it, server-side.
func (s *Service) UpdateNetwork(ctx context.Context, actor uuid.UUID, in NetworkInput) (State, error) {
	net, errs := normalizeNetwork(in.Network)
	if errs != nil {
		return State{}, errs
	}
	if !s.capabilities(ctx).Network {
		return State{}, ErrUnsupported
	}
	if _, err := s.reconcile(ctx); err != nil {
		return State{}, err
	}
	s.netLock.Lock()
	defer s.netLock.Unlock()
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	if rec.Pending != nil {
		return State{}, ErrPending
	}
	dnsOnly := false
	if s.host != nil {
		if live, ok := s.host.HostNetwork(ctx); ok {
			switch {
			case sameInterface(live, net):
				dnsOnly = true
			case riskyInterfaceChange(live, net) && !in.ConfirmInterfaceChange:
				return State{}, ErrRiskyNetwork
			}
		}
	}
	if err := s.host.ApplyNetwork(ctx, net, NetworkConfirmWindow, dnsOnly); err != nil {
		return State{}, err
	}
	changes := diff(rec.Network, net)
	rec.Pending = &Pending{Previous: rec.Network, Proposed: net, Deadline: s.now().Add(NetworkConfirmWindow)}
	rec.Network = net
	if err := s.repo.Save(ctx, actor, SectionNetwork, rec, changes); err != nil {
		// Without a stored deadline nobody could confirm: undo on the host.
		if rerr := s.host.RevertNetwork(ctx); rerr != nil {
			s.logger.Error("instance network revert after failed save", "error", rerr)
		}
		return State{}, err
	}
	s.logger.Info("instance network change applied, awaiting confirmation", "actor", actor, "changes", changes)
	return s.stateLocked(ctx, rec), nil
}

// ConfirmNetwork keeps the pending network change.
func (s *Service) ConfirmNetwork(ctx context.Context, actor uuid.UUID) (State, error) {
	return s.settle(ctx, actor, true)
}

// RevertNetwork cancels the pending network change now.
func (s *Service) RevertNetwork(ctx context.Context, actor uuid.UUID) (State, error) {
	return s.settle(ctx, actor, false)
}

func (s *Service) settle(ctx context.Context, actor uuid.UUID, confirm bool) (State, error) {
	if _, err := s.reconcile(ctx); err != nil {
		return State{}, err
	}
	s.netLock.Lock()
	defer s.netLock.Unlock()
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	if rec.Pending == nil {
		return State{}, ErrNoPending
	}
	event := "confirmed"
	if confirm {
		err = s.host.ConfirmNetwork(ctx)
	} else {
		event = "reverted by operator"
		if err = s.host.RevertNetwork(ctx); err == nil {
			rec.Network = rec.Pending.Previous
		}
	}
	if err != nil {
		return State{}, err
	}
	rec.Pending = nil
	if err := s.repo.Save(ctx, actor, SectionNetwork, rec, map[string]any{"event": event}); err != nil {
		return State{}, err
	}
	s.logger.Info("instance network change settled", "actor", actor, "event", event)
	return s.stateLocked(ctx, rec), nil
}

func (s *Service) stateLocked(ctx context.Context, rec Stored) State {
	return State{
		General: s.general(rec), Network: rec.Network, System: rec.System,
		Capabilities: s.capabilities(ctx), Pending: rec.Pending,
	}
}

// diff returns {field: {from, to}} for the audit trail; for struct values it
// compares the JSON-visible fields as a whole.
func diff(from, to any) map[string]any {
	if fm, ok := from.(map[string]any); ok {
		tm := to.(map[string]any)
		out := map[string]any{}
		for k, v := range tm {
			if fm[k] != v {
				out[k] = map[string]any{"from": fm[k], "to": v}
			}
		}
		return out
	}
	if reflect.DeepEqual(from, to) {
		return map[string]any{}
	}
	return map[string]any{"from": from, "to": to}
}
