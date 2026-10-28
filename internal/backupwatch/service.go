package backupwatch

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
)

const (
	// EvalInterval is the control-plane read-model cadence. It never polls a
	// node and never depends on the UI being open.
	EvalInterval = time.Minute
)

// IncidentReporter is the slice of the incident lifecycle this package uses.
type IncidentReporter interface {
	Report(context.Context, incidents.Observation) (*incidents.Incident, incidents.Transition, error)
	Close(context.Context, string, string) error
}

// LeaseReader reports whether the control plane still holds the lease for the
// exact backup run. A live lease never bypasses the MaxRun ceiling, and a
// different run cannot shadow an orphaned row.
type LeaseReader interface {
	BackupRunActive(databaseID, backupID uuid.UUID) bool
}

type Config struct {
	Store     Source
	Incidents IncidentReporter
	Leases    LeaseReader
	Logger    *slog.Logger
	Now       func() time.Time
	Interval  time.Duration
}

type Service struct {
	source    Source
	incidents IncidentReporter
	leases    LeaseReader
	logger    *slog.Logger
	now       func() time.Time
	interval  time.Duration
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = EvalInterval
	}
	return &Service{
		source:    cfg.Store,
		incidents: cfg.Incidents,
		leases:    cfg.Leases,
		logger:    logger,
		now:       now,
		interval:  interval,
	}
}

func (s *Service) ready() error {
	if s == nil || s.source == nil || s.incidents == nil {
		return errors.New("backupwatch: source and incidents are required")
	}
	return nil
}

// Run evaluates once per minute until ctx ends.
func (s *Service) Run(ctx context.Context) {
	if s.ready() != nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			evalCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			s.tick(evalCtx)
			cancel()
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	schedules, err := s.source.ProtectionSchedules(ctx)
	if err != nil {
		s.logger.Warn("backupwatch: list schedules", "error", err)
		return
	}
	active, err := s.source.ActiveProtectionIncidents(ctx)
	if err != nil {
		s.logger.Warn("backupwatch: list active incidents", "error", err)
		return
	}
	activeSet := make(map[string]bool, len(active))
	for _, identity := range active {
		activeSet[identity] = true
	}
	known := make(map[string]bool, len(schedules))
	now := s.now().UTC()
	for _, schedule := range schedules {
		identity := Identity(schedule.ScheduleID)
		known[identity] = true
		if !schedule.Enabled {
			if activeSet[identity] {
				s.close(ctx, identity, "schedule_disabled")
			}
			continue
		}
		state := s.leaseState(schedule.DatabaseID, schedule.RunningID)
		evaluated := Evaluate(now, schedule, state)
		if activeSet[identity] {
			evaluated = EvaluateOpen(now, schedule, state)
		}
		s.observe(ctx, schedule, evaluated, now)
	}
	for _, identity := range active {
		if known[identity] {
			continue
		}
		s.close(ctx, identity, "schedule_removed")
	}
}

func (s *Service) observe(ctx context.Context, schedule ScheduleEvidence, evaluated Evaluation, now time.Time) {
	switch evaluated.Status {
	case StatusProtected:
		s.report(ctx, schedule, evaluated, now, true, false, incidents.SeverityWarning)
	case StatusOverdue:
		s.report(ctx, schedule, evaluated, now, true, true, incidents.SeverityWarning)
	case StatusStuck:
		s.report(ctx, schedule, evaluated, now, true, true, incidents.SeverityCritical)
	case StatusRunning:
		// A live or still-fresh run defers the deadline but never counts as
		// recovery. Unknown evidence keeps an already firing incident open.
		s.report(ctx, schedule, evaluated, now, false, false, "")
	}
}

func (s *Service) report(ctx context.Context, schedule ScheduleEvidence, evaluated Evaluation, now time.Time, valid, unhealthy bool, severity incidents.Severity) {
	scopeKind, scopeID := incidents.ScopeGlobal, schedule.DatabaseID
	if schedule.ProjectID != uuid.Nil {
		scopeKind, scopeID = incidents.ScopeProject, schedule.ProjectID
	}
	observation := incidents.Observation{
		Identity:     Identity(schedule.ScheduleID),
		Detector:     Detector,
		ScopeKind:    scopeKind,
		ScopeID:      scopeID,
		ProjectID:    schedule.ProjectID,
		ResourceType: ResourceDatabase,
		ResourceID:   schedule.DatabaseID,
		ResourceName: schedule.DatabaseName,
		At:           now,
		Valid:        valid,
		Unhealthy:    unhealthy,
		Severity:     severity,
		Evidence:     evaluated.Evidence,
	}
	if valid && unhealthy {
		observation.Unhealthy = true
	}
	_, _, err := s.incidents.Report(ctx, observation)
	if err != nil {
		s.logger.Warn("backupwatch: report protection",
			"schedule_id", schedule.ScheduleID.String(), "status", evaluated.Status, "error", err)
	}
}

func (s *Service) close(ctx context.Context, identity, reason string) {
	if err := s.incidents.Close(ctx, identity, reason); err != nil {
		s.logger.Warn("backupwatch: close protection", "identity", identity, "reason", reason, "error", err)
	}
}

func (s *Service) leaseState(databaseID, backupID uuid.UUID) LeaseState {
	if s.leases == nil {
		return LeaseUnknown
	}
	if s.leases.BackupRunActive(databaseID, backupID) {
		return LeaseLive
	}
	return LeaseExpired
} // Summary returns the API view for one database. It reads the same evidence as
// the observer and never mutates a schedule or a job.
func (s *Service) Summary(ctx context.Context, databaseID uuid.UUID) (Summary, error) {
	if s == nil || s.source == nil {
		return Summary{}, errors.New("backupwatch: source is not configured")
	}
	db, err := s.source.Database(ctx, databaseID)
	if err != nil {
		return Summary{}, err
	}
	schedules, err := s.source.ProtectionSchedules(ctx)
	if err != nil {
		return Summary{}, err
	}
	active, err := s.source.ActiveProtectionIncidents(ctx)
	if err != nil {
		return Summary{}, err
	}
	openIDs := make(map[uuid.UUID]bool, len(active))
	byIdentity := make(map[string]uuid.UUID, len(schedules))
	for _, schedule := range schedules {
		byIdentity[Identity(schedule.ScheduleID)] = schedule.ScheduleID
	}
	for _, identity := range active {
		if scheduleID, ok := byIdentity[identity]; ok {
			openIDs[scheduleID] = true
		}
	}
	rows := make([]ScheduleEvidence, 0, len(schedules))
	for _, schedule := range schedules {
		if schedule.DatabaseID == databaseID {
			rows = append(rows, schedule)
		}
	}
	summary := Summarize(rows, s.now().UTC(), s.leaseState, func(id uuid.UUID) bool {
		return openIDs[id]
	})
	summary.DatabaseID = db.ID.String()
	return summary, nil
}
