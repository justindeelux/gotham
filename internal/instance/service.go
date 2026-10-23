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

type Service struct {
	SyncPanelCertificates func(context.Context, []string) error
	ResyncProxy           func(context.Context) error
	CheckControlPlaneHost func(context.Context, string) error
	domainLock            sync.Mutex
	domainTimer           *time.Timer
	domainClosed          bool
	repo                  Repository
	host                  HostApplier
	logger                *slog.Logger
	now                   func() time.Time
	lookup                func(string) string
	netLock               sync.Mutex
}

func NewService(repo Repository, host HostApplier, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, host: host, logger: logger, now: time.Now, lookup: os.Getenv}
}

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

func (s *Service) ControlPlaneURL(ctx context.Context) (string, error) {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return "", err
	}
	return s.general(rec).ControlPlaneURL.Value, nil
}

func (s *Service) hostState(ctx context.Context) Host {
	if s.host == nil {
		return Host{}
	}
	return s.host.Probe(ctx)
}

func (s *Service) Get(ctx context.Context) (State, error) {
	if err := s.reconcileControlPlaneURL(ctx); err != nil {
		return State{}, err
	}
	rec, err := s.reconcile(ctx)
	if err != nil {
		return State{}, err
	}
	if rec.Pending == nil {
		if live, ok := s.hostLive(ctx); ok {
			rec.Network.IPv4, rec.Network.IPv6 = live.IPv4, live.IPv6
			if len(live.DNSServers) > 0 {
				rec.Network.DNSServers = live.DNSServers
			}
		}
	}
	host := s.hostState(ctx)
	return State{
		General: s.general(rec), Network: rec.Network, System: rec.System,
		Capabilities: Capabilities{Network: host.Network, System: host.System},
		Host:         host, Pending: rec.Pending, PendingControlPlaneURL: rec.PendingControlPlaneURL,
	}, nil
}

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

func sameInterface(live HostNetwork, req Network) bool {
	return live.IPv4 == req.IPv4 && live.IPv6 == req.IPv6
}

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

func (s *Service) Reconcile(ctx context.Context) error {
	if err := s.reconcileControlPlaneURL(ctx); err != nil {
		return err
	}
	_, err := s.reconcile(ctx)
	return err
}

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

func (s *Service) UpdateGeneral(ctx context.Context, actor uuid.UUID, in GeneralInput) (State, error) {
	if err := s.reconcileControlPlaneURL(ctx); err != nil {
		return State{}, err
	}
	s.domainLock.Lock()
	defer s.domainLock.Unlock()
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
	changedURL := cur.ControlPlaneURL.Value != in.ControlPlaneURL
	if changedURL && rec.PendingControlPlaneURL != nil {
		return State{}, ErrDomainPending
	}
	if changedURL && s.CheckControlPlaneHost != nil {
		if err := s.CheckControlPlaneHost(ctx, in.ControlPlaneURL); err != nil {
			return State{}, FieldErrors{"control_plane_url": err.Error()}
		}
	}
	next := rec
	if changedURL {
		next.PendingControlPlaneURL = &PendingControlPlaneURL{Previous: rec.ControlPlaneURL, Proposed: in.ControlPlaneURL, Deadline: s.now().Add(ControlPlaneConfirmWindow)}
	}
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
	if !changedURL {
		s.syncPanelCertificates(ctx, next)
	}
	if changedURL {
		s.scheduleDomainRevert(next.PendingControlPlaneURL.Deadline)
		if err := s.syncControlPlaneURL(ctx); err != nil {
			s.logger.Warn("instance control-plane URL pending; proxy resync failed", "error", err)
		} else {
			pending := *next.PendingControlPlaneURL
			pending.Synced = true
			next.PendingControlPlaneURL = &pending
			if err := s.repo.Save(ctx, actor, SectionGeneral, next, map[string]any{"event": "control-plane routers synced"}); err != nil {
				return State{}, err
			}
		}
	}
	return s.stateLocked(ctx, next), nil
}

func ptrOrNil(v string, locked bool, stored *string) *string {
	if locked {
		return stored
	}
	if v == "" {
		return nil
	}
	return &v
}

func (s *Service) UpdateSystem(ctx context.Context, actor uuid.UUID, in System) (State, error) {
	in, errs := normalizeSystem(in)
	if errs != nil {
		return State{}, errs
	}
	if !s.hostState(ctx).System {
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

func (s *Service) UpdateNetwork(ctx context.Context, actor uuid.UUID, in NetworkInput) (State, error) {
	net, errs := normalizeNetwork(in.Network)
	if errs != nil {
		return State{}, errs
	}
	if !s.hostState(ctx).Network {
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
		if rerr := s.host.RevertNetwork(ctx); rerr != nil {
			s.logger.Error("instance network revert after failed save", "error", rerr)
		}
		return State{}, err
	}
	s.logger.Info("instance network change applied, awaiting confirmation", "actor", actor, "changes", changes)
	return s.stateLocked(ctx, rec), nil
}

func (s *Service) ConfirmNetwork(ctx context.Context, actor uuid.UUID) (State, error) {
	return s.settle(ctx, actor, true)
}

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
	host := s.hostState(ctx)
	return State{
		General: s.general(rec), Network: rec.Network, System: rec.System,
		Capabilities: Capabilities{Network: host.Network, System: host.System},
		Host:         host, Pending: rec.Pending, PendingControlPlaneURL: rec.PendingControlPlaneURL,
	}
}

func (s *Service) InstallHostHelper(ctx context.Context, actor uuid.UUID) (State, string, error) {
	if s.host == nil {
		return State{}, "", ErrUnsupported
	}
	probe := s.host.Probe(ctx)
	if probe.Helper == HelperReady {
		st, err := s.Get(ctx)
		return st, "", err
	}
	if !probe.Repair {
		s.logger.Info("instance host helper install refused: no repair path",
			"actor", actor, "helper", probe.Helper)
		return State{}, "", ErrUnsupported
	}
	log, err := s.host.InstallHostHelper(ctx)
	s.logger.Info("instance host helper install",
		"actor", actor, "ok", err == nil, "helper_before", probe.Helper, "output", strings.TrimSpace(log))
	if err != nil {
		return State{}, log, err
	}
	st, err := s.Get(ctx)
	return st, log, err
}

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
