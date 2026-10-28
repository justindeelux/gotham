package domainwatch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// ProbeRunner performs one probe; tests substitute a deterministic runner.
type ProbeRunner interface {
	Probe(context.Context, Target) Result
}

// IncidentReporter is the slice of the incident lifecycle this package uses.
type IncidentReporter interface {
	Report(context.Context, incidents.Observation) (*incidents.Incident, incidents.Transition, error)
	Close(context.Context, string, string) error
}

type Config struct {
	Store     *store.Store
	Incidents IncidentReporter
	Logger    *slog.Logger
	Now       func() time.Time
	Prober    ProbeRunner
	Interval  time.Duration
	Grace     time.Duration
	Retention time.Duration
}

type Service struct {
	st        *store.Store
	incidents IncidentReporter
	logger    *slog.Logger
	now       func() time.Time
	prober    ProbeRunner
	interval  time.Duration
	grace     time.Duration
	retention time.Duration
	startedAt time.Time

	mu         sync.Mutex
	nextAt     map[uuid.UUID]time.Time
	suppressed map[uuid.UUID]string
	lastPrune  time.Time
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
	prober := cfg.Prober
	if prober == nil {
		prober = NewProber()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = ProbeInterval
	}
	grace := cfg.Grace
	if grace <= 0 {
		grace = StartupGrace
	}
	retention := cfg.Retention
	if retention <= 0 {
		retention = Retention
	}
	return &Service{
		st:         cfg.Store,
		incidents:  cfg.Incidents,
		logger:     logger,
		now:        now,
		prober:     prober,
		interval:   interval,
		grace:      grace,
		retention:  retention,
		startedAt:  now().UTC(),
		nextAt:     map[uuid.UUID]time.Time{},
		suppressed: map[uuid.UUID]string{},
	}
}

func (s *Service) ready() error {
	if s == nil || s.st == nil || s.incidents == nil {
		return errors.New("domainwatch: store and incidents are required")
	}
	return nil
}

// Run probes independent of UI activity. The ticker only checks which
// domains are due; each probe has its own timeout and bounded concurrency.
func (s *Service) Run(ctx context.Context) {
	if s.ready() != nil {
		return
	}
	tick := s.interval / 4
	if tick < time.Second {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	targets, err := s.st.ListDomainProbeTargets(ctx)
	if err != nil {
		s.logger.Warn("domainwatch: list targets", "error", err)
		return
	}
	known := make(map[uuid.UUID]bool, len(targets))
	for _, row := range targets {
		known[row.ID.Bytes] = true
	}
	s.closeRemovedIncidents(ctx, known)
	s.prune(ctx)

	now := s.now().UTC()
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for _, row := range targets {
		target := targetFromRow(row)
		if reason := suppressionReason(target); reason != "" {
			s.suppress(ctx, target, reason)
			continue
		}
		s.mu.Lock()
		delete(s.suppressed, target.DomainID)
		s.mu.Unlock()

		if deploymentInProgress(target, now) {
			continue
		}
		if now.Before(s.graceUntil(target)) {
			continue
		}
		s.mu.Lock()
		next, scheduled := s.nextAt[target.DomainID]
		if !scheduled {
			next = now
		}
		if now.Before(next) {
			s.mu.Unlock()
			continue
		}
		s.nextAt[target.DomainID] = now.Add(s.interval + jitterFor(target.DomainID))
		s.mu.Unlock()

		select {
		case sem <- struct{}{}:
			wg.Add(1)
			go func(target Target) {
				defer wg.Done()
				defer func() { <-sem }()
				result := s.prober.Probe(ctx, target)
				if err := s.record(ctx, target, result); err != nil {
					s.logger.Warn("domainwatch: record probe",
						"domain_id", target.DomainID.String(), "error", err)
				}
			}(target)
		default:
			// The concurrency bound is full; retry this domain on the next
			// tick instead of queueing unbounded probes.
			s.mu.Lock()
			s.nextAt[target.DomainID] = now.Add(5 * time.Second)
			s.mu.Unlock()
		}
	}
	wg.Wait()
}

func (s *Service) closeRemovedIncidents(ctx context.Context, known map[uuid.UUID]bool) {
	active, err := s.st.ListActiveDomainIncidents(ctx)
	if err != nil {
		s.logger.Warn("domainwatch: list active domain incidents", "error", err)
		return
	}
	for _, row := range active {
		if row.ResourceID.Valid && known[row.ResourceID.Bytes] {
			continue
		}
		if err := s.incidents.Close(ctx, row.IdentityKey, "domain_removed"); err != nil {
			s.logger.Warn("domainwatch: close removed domain incident", "error", err)
		}
	}
}

// suppress closes an incident once per reason and records no sample: a
// stopped, never-deployed or disabled application is an unknown gap, not a
// target failure.
func (s *Service) suppress(ctx context.Context, target Target, reason string) {
	s.mu.Lock()
	previous := s.suppressed[target.DomainID]
	s.suppressed[target.DomainID] = reason
	s.mu.Unlock()
	if previous != reason {
		s.closeIncident(ctx, target, reason)
	}
}

func suppressionReason(target Target) string {
	switch {
	case !target.Settings.Enabled:
		return "monitoring_disabled"
	case target.DesiredState == "stopped":
		return "intentionally_stopped"
	case !target.HasDeployment:
		return "not_deployed"
	default:
		return ""
	}
}

func (s *Service) prune(ctx context.Context) {
	now := s.now().UTC()
	s.mu.Lock()
	due := s.lastPrune.IsZero() || now.Sub(s.lastPrune) >= time.Hour
	if due {
		s.lastPrune = now
	}
	s.mu.Unlock()
	if !due {
		return
	}
	if _, err := s.st.DeleteDomainProbeResultsBefore(ctx, pgTimestamptz(now.Add(-s.retention))); err != nil {
		s.logger.Warn("domainwatch: prune results", "error", err)
	}
}

func (s *Service) graceUntil(target Target) time.Time {
	until := s.startedAt.Add(s.grace)
	for _, at := range []time.Time{target.CreatedAt, target.DomainUpdatedAt, target.SettingsUpdated} {
		if !at.IsZero() && at.Add(s.grace).After(until) {
			until = at.Add(s.grace)
		}
	}
	// A runtime start action already stores its own grace end.
	if target.RuntimeGraceUntil.After(until) {
		until = target.RuntimeGraceUntil
	}
	// A release that just finished gets the same grace as a configuration
	// change. The gap before the next scheduled probe is therefore longer
	// than maxValidGap, so the rollout is unknown time, not false uptime.
	if target.DeploymentState == "running" && target.DeploymentFinished != nil {
		if at := target.DeploymentFinished.Add(s.grace); at.After(until) {
			until = at
		}
	}
	return until
}

// deploymentInProgress skips probes while a release is building or starting.
// A deployment that stopped moving no longer hides real failures.
func deploymentInProgress(target Target, now time.Time) bool {
	switch target.DeploymentState {
	case "", "running", "failed":
		return false
	}
	if target.DeploymentStarted == nil {
		return true
	}
	return now.Sub(*target.DeploymentStarted) < 10*time.Minute
}

func (s *Service) record(ctx context.Context, target Target, result Result) error {
	at := result.At.UTC()
	prev, err := s.st.GetDomainMonitorState(ctx, pgUUID(target.DomainID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var lastCompleted time.Time
	if err == nil && prev.LastCompletedAt.Valid {
		lastCompleted = prev.LastCompletedAt.Time.UTC()
	}
	// A gap larger than one probe interval plus tolerance is unknown time.
	// Starting the consecutive counters from zero prevents a failure streak
	// from before a restart or a grace window from opening an incident.
	if !lastCompleted.IsZero() && at.Sub(lastCompleted) > maxValidGap {
		prev.ConsecutiveFailures = 0
		prev.ConsecutiveSuccesses = 0
	}
	intervalStart := at
	var validSeconds int64
	if !lastCompleted.IsZero() && at.After(lastCompleted) && at.Sub(lastCompleted) <= maxValidGap {
		intervalStart = lastCompleted
		validSeconds = int64(at.Sub(lastCompleted) / time.Second)
	}

	failures := int(prev.ConsecutiveFailures)
	successes := int(prev.ConsecutiveSuccesses)
	switch result.Outcome {
	case OutcomeHealthy:
		failures = 0
		successes++
	case OutcomeFailed:
		successes = 0
		failures++
	default:
		failures = 0
		successes = 0
		validSeconds = 0
	}

	var status *int32
	if result.StatusCode > 0 {
		value := int32(result.StatusCode)
		status = &value
	}
	var latency *int32
	if result.Latency > 0 {
		value := int32(result.Latency / time.Millisecond)
		latency = &value
	}
	if result.Outcome == OutcomeUnsupported {
		validSeconds = 0
	}
	if validSeconds < 0 {
		validSeconds = 0
	}
	if err := s.st.InsertDomainProbeResult(ctx, sqlc.InsertDomainProbeResultParams{
		DomainID:      pgUUID(target.DomainID),
		ApplicationID: pgUUID(target.ApplicationID),
		ObservedAt:    pgTimestamptz(at),
		IntervalStart: pgTimestamptz(intervalStart),
		Outcome:       result.Outcome,
		Reason:        result.Reason,
		StatusCode:    status,
		LatencyMs:     latency,
		ValidSeconds:  int32(validSeconds),
	}); err != nil {
		return err
	}

	lastSuccess := prev.LastSuccessAt
	lastFailure := prev.LastFailureAt
	if result.Outcome == OutcomeHealthy && (status == nil || *status >= 200) {
		lastSuccess = pgTimestamptz(at)
	}
	if result.Outcome == OutcomeFailed {
		lastFailure = pgTimestamptz(at)
	}
	if _, err := s.st.UpsertDomainMonitorState(ctx, sqlc.UpsertDomainMonitorStateParams{
		DomainID:             pgUUID(target.DomainID),
		ConsecutiveFailures:  int32(failures),
		ConsecutiveSuccesses: int32(successes),
		LastOutcome:          result.Outcome,
		LastReason:           result.Reason,
		LastStatus:           status,
		LastLatencyMs:        latency,
		LastCheckedAt:        pgTimestamptz(at),
		LastCompletedAt:      pgTimestamptz(at),
		LastSuccessAt:        lastSuccess,
		LastFailureAt:        lastFailure,
		UpdatedAt:            pgTimestamptz(s.now().UTC()),
	}); err != nil {
		return err
	}

	s.report(ctx, target, result, failures, successes)
	return nil
}

// report hands the lifecycle only confirmed evidence: three consecutive
// completed failures open, two consecutive successes recover. Anything below
// those counts stays local and cannot fabricate a recovery.
func (s *Service) report(ctx context.Context, target Target, result Result, failures, successes int) {
	observation := incidents.Observation{
		Identity:     target.incidentIdentity(),
		Detector:     DetectorReachability,
		ScopeKind:    incidents.ScopeProject,
		ScopeID:      target.ProjectID,
		NodeID:       target.ServerID,
		ProjectID:    target.ProjectID,
		ResourceType: ResourceDomain,
		ResourceID:   target.DomainID,
		ResourceName: target.resourceName(),
		At:           result.At,
		Evidence:     result.evidence(),
	}
	switch result.Outcome {
	case OutcomeFailed:
		if failures < 3 {
			return
		}
		observation.Valid = true
		observation.Unhealthy = true
		observation.Severity = severityFor(result.Reason)
	case OutcomeHealthy:
		if successes < 2 {
			return
		}
		observation.Valid = true
	default:
		observation.Valid = false
	}
	if _, _, err := s.incidents.Report(ctx, observation); err != nil {
		s.logger.Warn("domainwatch: report incident",
			"domain_id", target.DomainID.String(), "error", err)
	}
}

func severityFor(reason string) incidents.Severity {
	switch reason {
	case ReasonUnexpectedStatus:
		return incidents.SeverityWarning
	default:
		return incidents.SeverityCritical
	}
}

func (s *Service) closeIncident(ctx context.Context, target Target, reason string) {
	if err := s.incidents.Close(ctx, target.incidentIdentity(), reason); err != nil {
		s.logger.Warn("domainwatch: close incident",
			"domain_id", target.DomainID.String(), "error", err)
	}
}

// Get returns one domain's settings, current status and bounded history.
func (s *Service) Get(ctx context.Context, appID, domainID uuid.UUID) (View, error) {
	if s == nil || s.st == nil {
		return View{}, errors.New("domainwatch: store is not configured")
	}
	target, err := s.target(ctx, appID, domainID)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, target)
}

func (s *Service) view(ctx context.Context, target Target) (View, error) {
	now := s.now().UTC()
	view := View{
		DomainID:       target.DomainID.String(),
		ApplicationID:  target.ApplicationID.String(),
		ProbeLocation:  "control_plane",
		Enabled:        target.Settings.Enabled,
		Path:           target.Settings.Path,
		ExpectedMin:    target.Settings.ExpectedMin,
		ExpectedMax:    target.Settings.ExpectedMax,
		TimeoutSeconds: int(target.Settings.Timeout / time.Second),
		Status:         StatusUnknown,
		Coverage:       "unknown",
		Windows:        []Window{},
	}
	if rawURL, err := buildURL(target); err == nil {
		view.URL = rawURL
	}
	state, err := s.st.GetDomainMonitorState(ctx, pgUUID(target.DomainID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return View{}, err
	}
	if err == nil {
		if state.LastCheckedAt.Valid {
			at := state.LastCheckedAt.Time.UTC()
			view.LastCheckedAt = &at
			if now.Sub(at) <= 2*maxValidGap {
				view.Coverage = "fresh"
			}
		}
		if state.LastSuccessAt.Valid {
			at := state.LastSuccessAt.Time.UTC()
			view.LastSuccessAt = &at
		}
		if state.LastFailureAt.Valid {
			at := state.LastFailureAt.Time.UTC()
			view.LastFailureAt = &at
		}
		if state.LastStatus != nil {
			value := int(*state.LastStatus)
			view.LastStatus = &value
		}
		if state.LastLatencyMs != nil {
			value := int(*state.LastLatencyMs)
			view.LastLatencyMS = &value
		}
		view.Reason = state.LastReason
		switch state.LastOutcome {
		case OutcomeHealthy:
			view.Status = StatusHealthy
		case OutcomeFailed:
			view.Status = StatusFailing
		case OutcomeUnsupported:
			view.Status = StatusUnsupported
			view.Coverage = "unknown"
		}
	}
	if !target.Settings.Enabled {
		view.Status = StatusDisabled
		view.Coverage = "unknown"
	}
	if active, listErr := s.st.ListActiveDomainIncidents(ctx); listErr == nil {
		for _, row := range active {
			if row.ResourceID.Valid && row.ResourceID.Bytes == target.DomainID {
				view.IncidentID = uuid.UUID(row.ID.Bytes).String()
				break
			}
		}
	}
	rows, listErr := s.st.ListDomainProbeResults(ctx, sqlc.ListDomainProbeResultsParams{
		DomainID:   pgUUID(target.DomainID),
		ObservedAt: pgTimestamptz(now.Add(-window7d - maxValidGap)),
		Limit:      maxHistoryRows,
	})
	if listErr != nil {
		return View{}, listErr
	}
	samples := make([]sample, 0, len(rows))
	for _, row := range rows {
		item := sample{
			At:           row.ObservedAt.Time.UTC(),
			Outcome:      row.Outcome,
			ValidSeconds: int64(row.ValidSeconds),
		}
		if row.IntervalStart.Valid {
			item.IntervalStart = row.IntervalStart.Time.UTC()
		}
		if row.LatencyMs != nil {
			item.Latency = time.Duration(*row.LatencyMs) * time.Millisecond
		}
		samples = append(samples, item)
	}
	view.Windows = buildWindows(now, samples)
	return view, nil
}

// SaveSettings validates and persists one domain's controls. The saved
// updated_at arms the configuration grace; history is not re-evaluated.
func (s *Service) SaveSettings(ctx context.Context, appID, domainID uuid.UUID, in SettingsInput) (View, error) {
	if s == nil || s.st == nil {
		return View{}, errors.New("domainwatch: store is not configured")
	}
	target, err := s.target(ctx, appID, domainID)
	if err != nil {
		return View{}, err
	}
	settings, err := in.validate()
	if err != nil {
		return View{}, err
	}
	now := s.now().UTC()
	if _, err := s.st.UpsertDomainMonitorSettings(ctx, sqlc.UpsertDomainMonitorSettingsParams{
		DomainID:       pgUUID(target.DomainID),
		Enabled:        settings.Enabled,
		Path:           settings.Path,
		ExpectedMin:    int32(settings.ExpectedMin),
		ExpectedMax:    int32(settings.ExpectedMax),
		TimeoutSeconds: int32(settings.Timeout / time.Second),
		UpdatedAt:      pgTimestamptz(now),
	}); err != nil {
		return View{}, err
	}
	// A configuration change makes the interval that spans it unknown: the
	// next completed probe starts a fresh observation instead of carrying
	// the pre-change streak into the new settings.
	if err := s.st.ResetDomainMonitorStateCompletion(ctx, sqlc.ResetDomainMonitorStateCompletionParams{
		DomainID:  pgUUID(target.DomainID),
		UpdatedAt: pgTimestamptz(now),
	}); err != nil {
		return View{}, err
	}
	target.Settings = settings
	target.SettingsUpdated = now
	s.mu.Lock()
	delete(s.nextAt, target.DomainID)
	s.mu.Unlock()
	return s.view(ctx, target)
}

// Test runs one probe without writing scheduled history or touching an
// incident.
func (s *Service) Test(ctx context.Context, appID, domainID uuid.UUID) (Result, error) {
	target, err := s.target(ctx, appID, domainID)
	if err != nil {
		return Result{}, err
	}
	return s.prober.Probe(ctx, target), nil
}

func (s *Service) target(ctx context.Context, appID, domainID uuid.UUID) (Target, error) {
	if appID == uuid.Nil || domainID == uuid.Nil {
		return Target{}, pgx.ErrNoRows
	}
	row, err := s.st.GetDomainProbeTarget(ctx, sqlc.GetDomainProbeTargetParams{
		ApplicationID: pgUUID(appID),
		ID:            pgUUID(domainID),
	})
	if err != nil {
		return Target{}, err
	}
	return targetFromRow(sqlc.ListDomainProbeTargetsRow(row)), nil
}

func targetFromRow(row sqlc.ListDomainProbeTargetsRow) Target {
	target := Target{
		DomainID:        row.ID.Bytes,
		ApplicationID:   row.ApplicationID.Bytes,
		ApplicationName: row.ApplicationName,
		Domain:          row.Domain,
		Protocol:        row.Protocol,
		ProjectID:       row.ProjectID.Bytes,
		ServerID:        uuid.UUID(row.ServerID.Bytes),
		Settings: Settings{
			Enabled:     row.Enabled,
			Path:        row.Path,
			ExpectedMin: int(row.ExpectedMin),
			ExpectedMax: int(row.ExpectedMax),
			Timeout:     time.Duration(row.TimeoutSeconds) * time.Second,
		},
	}
	if row.CreatedAt.Valid {
		target.CreatedAt = row.CreatedAt.Time.UTC()
	}
	if row.DomainUpdatedAt.Valid {
		target.DomainUpdatedAt = row.DomainUpdatedAt.Time.UTC()
	}
	if row.SettingsUpdatedAt.Valid {
		target.SettingsUpdated = row.SettingsUpdatedAt.Time.UTC()
	}
	target.DeploymentState = row.DeploymentState
	target.HasDeployment = row.HasDeployment
	target.DesiredState = row.DesiredState
	if row.RuntimeGraceUntil.Valid {
		target.RuntimeGraceUntil = row.RuntimeGraceUntil.Time.UTC()
	}
	if row.DeploymentStartedAt.Valid {
		at := row.DeploymentStartedAt.Time.UTC()
		target.DeploymentStarted = &at
	}
	if row.DeploymentFinishedAt.Valid {
		at := row.DeploymentFinishedAt.Time.UTC()
		target.DeploymentFinished = &at
	}
	return target
}

func jitterFor(id uuid.UUID) time.Duration {
	offset := (int(id[0]) + int(id[len(id)-1])) % 21
	return time.Duration(offset-10) * time.Second
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: id != uuid.Nil}
}

func pgTimestamptz(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at.UTC(), Valid: !at.IsZero()}
}
