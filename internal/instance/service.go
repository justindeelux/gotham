package instance

import (
	"context"
	"log/slog"
	"os"
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
	logger                *slog.Logger
	now                   func() time.Time
	lookup                func(string) string
}

func NewService(repo Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, logger: logger, now: time.Now, lookup: os.Getenv}
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
	}
}

// Timezone is the control plane's own timezone: schedules, log display and
// backups run in it. It is the local node's timezone, kept in the same row as
// before so no existing value is lost.
func (s *Service) Timezone(ctx context.Context) (string, error) {
	field, err := s.TimezoneField(ctx)
	return field.Value, err
}

func (s *Service) TimezoneField(ctx context.Context) (Field, error) {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return Field{}, err
	}
	return s.resolve(EnvTimezone, rec.Timezone, DefaultTimezone), nil
}

// SetTimezone stores the local node's timezone. A GOTHAM_TIMEZONE override
// wins, so the stored value may not diverge from what the control plane uses.
func (s *Service) SetTimezone(ctx context.Context, actor uuid.UUID, tz string) error {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return err
	}
	cur := s.resolve(EnvTimezone, rec.Timezone, DefaultTimezone)
	if cur.Locked {
		if cur.Value != tz {
			return FieldErrors{"timezone": "locked by environment variable " + cur.EnvVar}
		}
		return nil
	}
	if tz == cur.Value {
		return nil
	}
	rec.Timezone = &tz
	return s.repo.Save(ctx, actor, SectionGeneral, rec, map[string]any{"timezone": map[string]any{"from": cur.Value, "to": tz}})
}

func (s *Service) ControlPlaneURL(ctx context.Context) (string, error) {
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return "", err
	}
	return s.general(rec).ControlPlaneURL.Value, nil
}

func (s *Service) Get(ctx context.Context) (State, error) {
	if err := s.reconcileControlPlaneURL(ctx); err != nil {
		return State{}, err
	}
	rec, err := s.repo.Load(ctx)
	if err != nil {
		return State{}, err
	}
	return s.stateLocked(rec), nil
}

func (s *Service) Reconcile(ctx context.Context) error {
	return s.reconcileControlPlaneURL(ctx)
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
	changes := diff(map[string]any{
		"control_plane_url": cur.ControlPlaneURL.Value, "instance_name": cur.InstanceName.Value,
	}, map[string]any{
		"control_plane_url": s.general(next).ControlPlaneURL.Value, "instance_name": s.general(next).InstanceName.Value,
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
	return s.stateLocked(next), nil
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

func (s *Service) stateLocked(rec Stored) State {
	return State{General: s.general(rec), PendingControlPlaneURL: rec.PendingControlPlaneURL}
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
	return map[string]any{"from": from, "to": to}
}
