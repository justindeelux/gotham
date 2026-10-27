package runtimewatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// IncidentReporter is the slice of the incident lifecycle this package uses.
// Tests provide a deterministic fake; production passes incidents.Service.
type IncidentReporter interface {
	Report(context.Context, incidents.Observation) (*incidents.Incident, incidents.Transition, error)
	Close(context.Context, string, string) error
}

type Config struct {
	Store     *store.Store
	Incidents IncidentReporter
	Logger    *slog.Logger
	Now       func() time.Time
}

type Service struct {
	st        *store.Store
	incidents IncidentReporter
	logger    *slog.Logger
	now       func() time.Time
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
	return &Service{st: cfg.Store, incidents: cfg.Incidents, logger: logger, now: now}
}

// Observe evaluates one accepted node inventory generation. It never fails
// the agent's observation: every error is logged so the next push retries.
func (s *Service) Observe(ctx context.Context, obs servers.NodeObservation) {
	if s == nil || s.st == nil || s.incidents == nil || obs.ServerID == uuid.Nil {
		return
	}
	now := s.now().UTC()
	observedAt := obs.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	targets, err := s.targets(ctx, obs.ServerID)
	if err != nil {
		s.logger.Warn("runtimewatch: list targets", "server_id", obs.ServerID.String(), "error", err)
		return
	}
	if len(targets) == 0 {
		return
	}
	rows, err := s.st.ListRuntimeMonitorReplicaStatesForNode(ctx, pgUUID(obs.ServerID))
	if err != nil {
		s.logger.Warn("runtimewatch: list replica state", "server_id", obs.ServerID.String(), "error", err)
		return
	}
	byResource := map[uuid.UUID][]ReplicaState{}
	for _, row := range rows {
		byResource[row.ResourceID.Bytes] = append(byResource[row.ResourceID.Bytes], decodeState(row))
	}
	for _, target := range targets {
		if err := s.evaluate(ctx, target, obs, observedAt, now, byResource[target.ResourceID]); err != nil {
			s.logger.Warn("runtimewatch: evaluate target",
				"resource_type", target.ResourceType,
				"resource_id", target.ResourceID.String(),
				"error", err)
		}
	}
}

// NodeUnknown marks a node's runtime evidence unknown without inventing
// missing containers. An offline node interrupts confirmation and recovery.
func (s *Service) NodeUnknown(ctx context.Context, serverID uuid.UUID, at time.Time) {
	if s == nil || s.st == nil || s.incidents == nil || serverID == uuid.Nil {
		return
	}
	if at.IsZero() {
		at = s.now().UTC()
	}
	rows, err := s.st.ListRuntimeMonitorReplicaStatesForNode(ctx, pgUUID(serverID))
	if err != nil {
		s.logger.Warn("runtimewatch: list replica state", "server_id", serverID.String(), "error", err)
		return
	}
	for _, row := range rows {
		target := targetForRow(row)
		state := decodeState(row)
		s.reportUnknown(ctx, target, state, at)
		s.setCoverage(ctx, row, CoverageUnknown)
	}
}

// NodeDeleted closes every runtime incident for the node and removes its
// detector state; a later node with the same id cannot inherit it.
func (s *Service) NodeDeleted(ctx context.Context, serverID uuid.UUID) {
	if s == nil || s.st == nil || s.incidents == nil || serverID == uuid.Nil {
		return
	}
	// The server row and its replica rows cascade on delete, so close the
	// incidents from the incident table itself rather than the detector state.
	active, err := s.st.ListActiveIncidentsForNode(ctx, pgUUID(serverID))
	if err != nil {
		s.logger.Warn("runtimewatch: list node incidents", "server_id", serverID.String(), "error", err)
	} else {
		for _, inc := range active {
			if err := s.incidents.Close(ctx, inc.IdentityKey, "node_deleted"); err != nil {
				s.logger.Warn("runtimewatch: close node incident", "server_id", serverID.String(), "error", err)
			}
		}
	}
	if err := s.st.DeleteRuntimeMonitorStateForNode(ctx, pgUUID(serverID)); err != nil {
		s.logger.Warn("runtimewatch: delete node state", "server_id", serverID.String(), "error", err)
	}
}

// SetDesiredState records an explicit start/stop. Starting arms the startup
// grace; stopping is intentional and suppresses failures until the user
// starts the resource again.
func (s *Service) SetDesiredState(ctx context.Context, resourceType string, resourceID uuid.UUID, desired string, grace time.Duration) error {
	if s == nil || s.st == nil || resourceID == uuid.Nil {
		return nil
	}
	if desired != "running" && desired != "stopped" {
		return fmt.Errorf("runtimewatch: invalid desired state %q", desired)
	}
	if desired == "running" && grace <= 0 {
		settings, err := s.settings(ctx, resourceType, resourceID)
		if err != nil {
			return err
		}
		grace = settings.Grace
	}
	var until pgtype.Timestamptz
	if desired == "running" && grace > 0 {
		until = pgtype.Timestamptz{Time: s.now().UTC().Add(grace), Valid: true}
	}
	return s.st.SetRuntimeMonitorDesiredState(ctx, sqlc.SetRuntimeMonitorDesiredStateParams{
		ResourceType: resourceType,
		ResourceID:   pgUUID(resourceID),
		DesiredState: desired,
		GraceUntil:   until,
	})
}

// SetSettings stores the owner-controlled switch and grace. A nil field is
// left unchanged, so the two controls can be saved independently.
func (s *Service) SetSettings(ctx context.Context, resourceType string, resourceID uuid.UUID, enabled *bool, graceSeconds *int) error {
	if s == nil || s.st == nil || resourceID == uuid.Nil {
		return nil
	}
	current, err := s.settings(ctx, resourceType, resourceID)
	if err != nil {
		return err
	}
	next := current
	if enabled != nil {
		next.Enabled = *enabled
	}
	if graceSeconds != nil {
		if *graceSeconds < MinGraceSeconds || *graceSeconds > MaxGraceSeconds {
			return fmt.Errorf("runtimewatch: grace seconds must be between %d and %d", MinGraceSeconds, MaxGraceSeconds)
		}
		next.Grace = time.Duration(*graceSeconds) * time.Second
	}
	return s.st.UpsertRuntimeMonitorSettings(ctx, sqlc.UpsertRuntimeMonitorSettingsParams{
		ResourceType: resourceType,
		ResourceID:   pgUUID(resourceID),
		Enabled:      next.Enabled,
		GraceSeconds: int32(next.Grace / time.Second),
	})
}

// CloseResource closes every runtime incident for a deleted application or
// database and removes its detector state. Deletion is not a health recovery;
// the incident is closed explicitly so it cannot outlive its resource.
func (s *Service) CloseResource(ctx context.Context, resourceType string, resourceID uuid.UUID, reason string) {
	if s == nil || s.st == nil || s.incidents == nil || resourceID == uuid.Nil {
		return
	}
	active, err := s.st.ListActiveIncidentsByResources(ctx, sqlc.ListActiveIncidentsByResourcesParams{
		ResourceType: resourceType,
		ResourceIds:  []pgtype.UUID{pgUUID(resourceID)},
	})
	if err != nil {
		s.logger.Warn("runtimewatch: list resource incidents", "resource_id", resourceID.String(), "error", err)
	} else {
		for _, inc := range active {
			if closeErr := s.incidents.Close(ctx, inc.IdentityKey, reason); closeErr != nil {
				s.logger.Warn("runtimewatch: close resource incident", "resource_id", resourceID.String(), "error", closeErr)
			}
		}
	}
	if err := s.st.DeleteRuntimeMonitorStateForResource(ctx, sqlc.DeleteRuntimeMonitorStateForResourceParams{
		ResourceType: resourceType,
		ResourceID:   pgUUID(resourceID),
	}); err != nil {
		s.logger.Warn("runtimewatch: delete resource state", "resource_id", resourceID.String(), "error", err)
	}
	if err := s.st.DeleteRuntimeMonitorSettings(ctx, sqlc.DeleteRuntimeMonitorSettingsParams{
		ResourceType: resourceType,
		ResourceID:   pgUUID(resourceID),
	}); err != nil {
		s.logger.Warn("runtimewatch: delete resource settings", "resource_id", resourceID.String(), "error", err)
	}
}

func (s *Service) evaluate(ctx context.Context, target Target, obs servers.NodeObservation, observedAt, now time.Time, previous []ReplicaState) error {
	suppression := suppressionReason(target, observedAt)
	if err := s.st.DeleteRuntimeMonitorStateOnOtherNodes(ctx, sqlc.DeleteRuntimeMonitorStateOnOtherNodesParams{
		ResourceType: target.ResourceType,
		ResourceID:   pgUUID(target.ResourceID),
		NodeID:       pgUUID(target.NodeID),
	}); err != nil {
		return err
	}
	if !target.Settings.Enabled {
		for _, state := range previous {
			s.closeReplica(ctx, target, state, "monitoring_disabled")
		}
		return nil
	}
	if !obs.Inventory.Complete {
		// An incomplete inventory can update coverage but can never prove a
		// container disappeared or recovered.
		for _, state := range previous {
			s.reportUnknown(ctx, target, state, observedAt)
			if err := s.storeState(ctx, target, state, CoverageUnknown, observedAt); err != nil {
				return err
			}
		}
		return nil
	}

	byKey := map[string]ReplicaState{}
	for _, state := range previous {
		byKey[stateKey(state.Service, state.Replica)] = state
	}
	replicas := associatedReplicas(target, obs.Inventory.Containers)
	seen := map[string]bool{}

	for _, ev := range replicas {
		key := stateKey(ev.Service, ev.Replica)
		seen[key] = true
		prev := byKey[key]
		next := evaluateReplica(ev, prev, observedAt, now, suppression)
		if suppression != "" {
			if err := s.storeState(ctx, target, next, CoverageComplete, observedAt); err != nil {
				return err
			}
			if suppression == "intentionally_stopped" {
				s.closeReplica(ctx, target, prev, suppression)
			}
			s.reportExpected(ctx, target, next, observedAt)
			continue
		}
		// OOM detection updates the persisted evidence before the row write.
		if err := s.reportOOM(ctx, target, ev, prev, &next, observedAt); err != nil {
			return err
		}
		if err := s.storeState(ctx, target, next, CoverageComplete, observedAt); err != nil {
			return err
		}
		if err := s.reportHealth(ctx, target, next, observedAt, obs.SamplingInterval); err != nil {
			return err
		}
		restartLoop := next.Reason == "restart_loop"
		restartWaiting := !restartLoop && !restartRecoveryAllowed(next.LastRestartAt, observedAt)
		if err := s.reportRestartEvidence(ctx, target, next, observedAt,
			restartLoop,
			next.State == "running" && next.Health != "unhealthy" && !restartWaiting,
			restartWaiting); err != nil {
			return err
		}
	}

	for key, state := range byKey {
		if seen[key] {
			continue
		}
		if suppression != "" {
			if err := s.deleteState(ctx, target, state); err != nil {
				return err
			}
			s.closeReplica(ctx, target, state, suppression)
			continue
		}
		// A complete inventory proves an expected replica is absent.
		missing := state
		missing.State = "missing"
		missing.Health = "unknown"
		missing.Reason = "missing"
		missing.Coverage = CoverageComplete
		missing.ContainerID = ""
		if err := s.storeState(ctx, target, missing, CoverageComplete, observedAt); err != nil {
			return err
		}
		if err := s.reportHealth(ctx, target, missing, observedAt, obs.SamplingInterval); err != nil {
			return err
		}
		if err := s.reportRestartEvidence(ctx, target, state, observedAt, false, false); err != nil {
			return err
		}
	}
	return nil
}

func evaluateReplica(ev replicaEvidence, prev ReplicaState, observedAt, now time.Time, suppression string) ReplicaState {
	c := ev.Container
	next := ReplicaState{
		Service:      ev.Service,
		Replica:      ev.Replica,
		ContainerID:  c.ID,
		DeploymentID: c.DeploymentID,
		State:        normalizedState(c.State),
		Health:       c.Health,
		Coverage:     CoverageComplete,
		RestartCount: c.RestartCount,
		OOMContainer: prev.OOMContainer,
		OOMAt:        prev.OOMAt,
		ObservedAt:   observedAt,
	}
	if next.Health == "" {
		next.Health = "unknown"
	}
	if suppression != "" {
		next.Reason = suppression
		if suppression == "intentionally_stopped" {
			next.State = "stopped"
		} else {
			next.State = "starting"
		}
		next.ObservedAt = observedAt
		return next
	}
	if oneShotCompleted(c) {
		next.State = "stopped"
		next.Reason = "one_shot_completed"
		next.ObservedAt = observedAt
		return next
	}
	events, loop, lastEvent := updateRestartWindow(prev, c, now)
	next.RestartEvents = events
	next.LastRestartAt = lastEvent
	switch next.State {
	case "running":
		switch c.Health {
		case "healthy":
			next.Reason = ""
		case "unhealthy":
			next.State = "unhealthy"
			next.Reason = "unhealthy"
		case "starting":
			next.State = "starting"
			next.Reason = "starting"
		default:
			next.Health = "unknown"
			next.Reason = "no_healthcheck"
		}
	case "restarting":
		next.State = "unhealthy"
		next.Reason = "restarting"
	case "created":
		next.State = "starting"
		next.Reason = "created"
	case "exited", "dead", "removing":
		next.State = "stopped"
		next.Reason = "stopped"
	case "paused":
		next.State = "paused"
		next.Reason = "paused"
	default:
		next.State = "unknown"
		next.Reason = "unknown_state"
	}
	if loop && next.State != "stopped" && next.State != "missing" {
		next.State = "unhealthy"
		next.Reason = "restart_loop"
	}
	next.ObservedAt = observedAt
	return next
}

func normalizedState(state string) string {
	switch state {
	case "running", "restarting", "created", "paused", "exited", "dead", "removing":
		return state
	default:
		return "unknown"
	}
}

func (s *Service) reportHealth(ctx context.Context, target Target, state ReplicaState, at time.Time, sampling time.Duration) error {
	obs := s.baseObservation(target, state, DetectorHealth, at, sampling)
	switch state.State {
	case "running":
		if state.Health == "healthy" {
			obs.Valid = true
			obs.RecoverFor = healthRecoverFor
		} else {
			// A running container without a healthcheck is not proven healthy,
			// so the summary stays unknown. It is still valid recovery
			// evidence: the container is running again after a stop or crash.
			obs.Valid = true
			obs.RecoverFor = healthRecoverFor
		}
	case "unhealthy", "paused":
		obs.Valid = true
		obs.Unhealthy = true
		obs.Severity = incidents.SeverityWarning
		obs.ConfirmFor = healthConfirmFor
		obs.RecoverFor = healthRecoverFor
	case "stopped":
		if state.Reason == "one_shot_completed" {
			obs.Expected = true
			break
		}
		obs.Valid = true
		obs.Unhealthy = true
		obs.Severity = incidents.SeverityCritical
		obs.ConfirmFor = healthConfirmFor
		obs.RecoverFor = healthRecoverFor
	case "missing":
		obs.Valid = true
		obs.Unhealthy = true
		obs.Severity = incidents.SeverityCritical
		obs.ConfirmFor = healthConfirmFor
		obs.RecoverFor = healthRecoverFor
	case "starting":
		obs.Expected = true
	default:
		obs.Valid = false
	}
	obs.Evidence = replicaEvidenceText(state)
	_, _, err := s.incidents.Report(ctx, obs)
	return err
}

func (s *Service) reportExpected(ctx context.Context, target Target, state ReplicaState, at time.Time) {
	obs := s.baseObservation(target, state, DetectorHealth, at, 0)
	obs.Expected = true
	obs.Evidence = replicaEvidenceText(state)
	_, _, err := s.incidents.Report(ctx, obs)
	if err != nil {
		s.logger.Warn("runtimewatch: report expected health", "resource_id", target.ResourceID.String(), "error", err)
	}
	restart := s.baseObservation(target, state, DetectorRestart, at, 0)
	restart.Expected = true
	restart.Evidence = replicaEvidenceText(state)
	if _, _, err := s.incidents.Report(ctx, restart); err != nil {
		s.logger.Warn("runtimewatch: report expected restart", "resource_id", target.ResourceID.String(), "error", err)
	}
}

func (s *Service) reportUnknown(ctx context.Context, target Target, state ReplicaState, at time.Time) {
	for _, detector := range []string{DetectorHealth, DetectorRestart, DetectorOOM} {
		obs := s.baseObservation(target, state, detector, at, 0)
		obs.Valid = false
		obs.Evidence = replicaEvidenceText(state)
		if _, _, err := s.incidents.Report(ctx, obs); err != nil {
			s.logger.Warn("runtimewatch: report unknown", "resource_id", target.ResourceID.String(), "error", err)
		}
	}
}

func (s *Service) reportRestartEvidence(ctx context.Context, target Target, state ReplicaState, at time.Time, loop, healthy bool, waiting ...bool) error {
	hold := len(waiting) > 0 && waiting[0]
	obs := s.baseObservation(target, state, DetectorRestart, at, 0)
	switch {
	case loop:
		obs.Valid = true
		obs.Unhealthy = true
		obs.Severity = incidents.SeverityWarning
		obs.Evidence = fmt.Sprintf("%s restarts=%d window=%s", replicaEvidenceText(state), len(state.RestartEvents), restartWindow)
	case hold:
		// The three-in-window condition has cleared, but the last restart is
		// still inside its quiet window; hold the incident open.
		obs.Valid = false
		obs.Evidence = fmt.Sprintf("%s last_restart=%s", replicaEvidenceText(state), state.LastRestartAt.UTC().Format(time.RFC3339))
	case healthy:
		obs.Valid = true
		obs.RecoverFor = 0
		obs.Evidence = replicaEvidenceText(state)
	default:
		obs.Valid = false
		obs.Evidence = replicaEvidenceText(state)
	}
	_, _, err := s.incidents.Report(ctx, obs)
	return err
}

func (s *Service) reportOOM(ctx context.Context, target Target, ev replicaEvidence, prev ReplicaState, next *ReplicaState, at time.Time) error {
	occurrence := oomOccurrence(prev, ev.Group)
	if occurrence != nil {
		next.OOMContainer = occurrence.ID
		next.OOMAt = at
		obs := s.baseObservation(target, *next, DetectorOOM, at, 0)
		obs.Valid = true
		obs.Unhealthy = true
		obs.Severity = incidents.SeverityCritical
		obs.ConfirmFor = 0
		obs.RecoverFor = 0
		obs.Evidence = fmt.Sprintf("%s oom_container=%s exit_code=%d", replicaEvidenceText(*next), occurrence.ID, occurrence.ExitCode)
		_, _, err := s.incidents.Report(ctx, obs)
		return err
	}
	if next.OOMContainer == "" {
		return nil
	}
	obs := s.baseObservation(target, *next, DetectorOOM, at, 0)
	obs.RecoverFor = 0
	if next.ContainerID == next.OOMContainer && next.State != "running" {
		// The same container is still stopped, restarting or unhealthy after
		// the kill; the incident stays firing until it runs again.
		obs.Valid = true
		obs.Unhealthy = true
		obs.Severity = incidents.SeverityCritical
	} else {
		obs.Valid = true
		obs.Unhealthy = false
	}
	obs.Evidence = replicaEvidenceText(*next)
	_, _, err := s.incidents.Report(ctx, obs)
	return err
}

func (s *Service) closeReplica(ctx context.Context, target Target, state ReplicaState, reason string) {
	for _, detector := range []string{DetectorHealth, DetectorRestart, DetectorOOM} {
		if err := s.incidents.Close(ctx, replicaIdentity(detector, target, state.Service, state.Replica), reason); err != nil {
			s.logger.Warn("runtimewatch: close incident", "resource_id", target.ResourceID.String(), "error", err)
		}
	}
}

func (s *Service) baseObservation(target Target, state ReplicaState, detector string, at time.Time, sampling time.Duration) incidents.Observation {
	scopeKind, scopeID := incidents.ScopeNode, target.NodeID
	if target.ProjectID != uuid.Nil {
		scopeKind, scopeID = incidents.ScopeProject, target.ProjectID
	}
	name := target.Name
	if state.Service != "" {
		name += " / " + state.Service
	}
	if state.Replica != "" {
		name += " #" + state.Replica
	}
	maxGap := incidents.DefaultMaxGap
	if sampling > 0 && 3*sampling > maxGap {
		maxGap = 3 * sampling
	}
	return incidents.Observation{
		Identity:     replicaIdentity(detector, target, state.Service, state.Replica),
		Detector:     detector,
		ScopeKind:    scopeKind,
		ScopeID:      scopeID,
		NodeID:       target.NodeID,
		ProjectID:    target.ProjectID,
		ResourceType: target.ResourceType,
		ResourceID:   target.ResourceID,
		ResourceName: name,
		At:           at,
		MaxGap:       maxGap,
	}
}

func replicaIdentity(detector string, target Target, service, replica string) string {
	return incidents.Identity(detector, target.ResourceType, target.ResourceID, service, replica)
}

func replicaEvidenceText(state ReplicaState) string {
	return fmt.Sprintf("service=%s replica=%s container=%s deployment=%s state=%s health=%s reason=%s coverage=%s",
		state.Service, state.Replica, state.ContainerID, state.DeploymentID, state.State, state.Health, state.Reason, state.Coverage)
}

func stateKey(service, replica string) string {
	return service + "\x00" + replica
}

func targetForRow(row sqlc.RuntimeMonitorReplica) Target {
	return Target{
		ResourceType: row.ResourceType,
		ResourceID:   row.ResourceID.Bytes,
		NodeID:       row.NodeID.Bytes,
		ProjectID:    uuid.Nil,
	}
}

func (s *Service) storeState(ctx context.Context, target Target, state ReplicaState, coverage string, observedAt time.Time) error {
	events, err := json.Marshal(state.RestartEvents)
	if err != nil {
		return fmt.Errorf("runtimewatch: encode restart events: %w", err)
	}
	var oomAt pgtype.Timestamptz
	if !state.OOMAt.IsZero() {
		oomAt = pgtype.Timestamptz{Time: state.OOMAt, Valid: true}
	}
	return s.st.UpsertRuntimeMonitorReplicaState(ctx, sqlc.UpsertRuntimeMonitorReplicaStateParams{
		NodeID:        pgUUID(target.NodeID),
		ResourceType:  target.ResourceType,
		ResourceID:    pgUUID(target.ResourceID),
		Service:       state.Service,
		Replica:       state.Replica,
		ContainerID:   state.ContainerID,
		DeploymentID:  state.DeploymentID,
		State:         state.State,
		Health:        state.Health,
		Reason:        state.Reason,
		Coverage:      coverage,
		RestartCount:  state.RestartCount,
		RestartEvents: events,
		OomContainer:  state.OOMContainer,
		OomAt:         oomAt,
		ObservedAt:    pgtype.Timestamptz{Time: observedAt, Valid: true},
	})
}

func (s *Service) deleteState(ctx context.Context, target Target, state ReplicaState) error {
	return s.st.DeleteRuntimeMonitorReplicaState(ctx, sqlc.DeleteRuntimeMonitorReplicaStateParams{
		NodeID:       pgUUID(target.NodeID),
		ResourceType: target.ResourceType,
		ResourceID:   pgUUID(target.ResourceID),
		Service:      state.Service,
		Replica:      state.Replica,
	})
}

func (s *Service) setCoverage(ctx context.Context, row sqlc.RuntimeMonitorReplica, coverage string) {
	if err := s.st.UpsertRuntimeMonitorReplicaState(ctx, sqlc.UpsertRuntimeMonitorReplicaStateParams{
		NodeID:        row.NodeID,
		ResourceType:  row.ResourceType,
		ResourceID:    row.ResourceID,
		Service:       row.Service,
		Replica:       row.Replica,
		ContainerID:   row.ContainerID,
		DeploymentID:  row.DeploymentID,
		State:         row.State,
		Health:        row.Health,
		Reason:        row.Reason,
		Coverage:      coverage,
		RestartCount:  row.RestartCount,
		RestartEvents: row.RestartEvents,
		OomContainer:  row.OomContainer,
		OomAt:         row.OomAt,
		ObservedAt:    row.ObservedAt,
	}); err != nil {
		s.logger.Warn("runtimewatch: update coverage", "resource_id", uuid.UUID(row.ResourceID.Bytes).String(), "error", err)
	}
}

func (s *Service) settings(ctx context.Context, resourceType string, resourceID uuid.UUID) (Settings, error) {
	row, err := s.st.GetRuntimeMonitorSettings(ctx, sqlc.GetRuntimeMonitorSettingsParams{
		ResourceType: resourceType,
		ResourceID:   pgUUID(resourceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DefaultSettings(), nil
		}
		return Settings{}, err
	}
	settings := Settings{
		Enabled:      row.Enabled,
		Grace:        time.Duration(row.GraceSeconds) * time.Second,
		DesiredState: row.DesiredState,
	}
	if row.GraceUntil.Valid {
		settings.GraceUntil = row.GraceUntil.Time.UTC()
	}
	return settings, nil
}

func (s *Service) targets(ctx context.Context, serverID uuid.UUID) ([]Target, error) {
	applications, err := s.st.ListRuntimeMonitorApplications(ctx, pgUUID(serverID))
	if err != nil {
		return nil, err
	}
	databases, err := s.st.ListRuntimeMonitorDatabases(ctx, pgUUID(serverID))
	if err != nil {
		return nil, err
	}
	out := make([]Target, 0, len(applications)+len(databases))
	for _, row := range applications {
		settings := Settings{
			Enabled:      row.Enabled,
			Grace:        time.Duration(row.GraceSeconds) * time.Second,
			DesiredState: row.DesiredState,
		}
		if row.GraceUntil.Valid {
			settings.GraceUntil = row.GraceUntil.Time.UTC()
		}
		target := Target{
			ResourceType:       ResourceApplication,
			ResourceID:         row.ID.Bytes,
			Name:               row.Name,
			NodeID:             serverID,
			ProjectID:          row.ProjectID.Bytes,
			Compose:            row.SourceType == "compose",
			Settings:           settings,
			DeploymentState:    row.DeploymentState,
			DeploymentStarted:  timePtr(row.DeploymentStartedAt),
			DeploymentFinished: timePtr(row.DeploymentFinishedAt),
		}
		out = append(out, target)
	}
	for _, row := range databases {
		settings := Settings{
			Enabled:      row.Enabled,
			Grace:        time.Duration(row.GraceSeconds) * time.Second,
			DesiredState: row.DesiredState,
		}
		if row.GraceUntil.Valid {
			settings.GraceUntil = row.GraceUntil.Time.UTC()
		}
		out = append(out, Target{
			ResourceType:   ResourceDatabase,
			ResourceID:     row.ID.Bytes,
			Name:           row.Name,
			NodeID:         serverID,
			ProjectID:      row.ProjectID.Bytes,
			Settings:       settings,
			DatabaseStatus: row.Status,
		})
	}
	return out, nil
}

func decodeState(row sqlc.RuntimeMonitorReplica) ReplicaState {
	state := ReplicaState{
		Service:      row.Service,
		Replica:      row.Replica,
		ContainerID:  row.ContainerID,
		DeploymentID: row.DeploymentID,
		State:        row.State,
		Health:       row.Health,
		Reason:       row.Reason,
		Coverage:     row.Coverage,
		RestartCount: row.RestartCount,
		OOMContainer: row.OomContainer,
	}
	_ = json.Unmarshal(row.RestartEvents, &state.RestartEvents)
	if row.OomAt.Valid {
		state.OOMAt = row.OomAt.Time.UTC()
	}
	if row.ObservedAt.Valid {
		state.ObservedAt = row.ObservedAt.Time.UTC()
	}
	return state
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time.UTC()
	return &at
}
