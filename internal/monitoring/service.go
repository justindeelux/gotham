package monitoring

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

const (
	HistoryRetention = 30 * 24 * time.Hour
	sweepInterval    = 30 * time.Second
	staleAfter       = 45 * time.Second
)

type IncidentReporter interface {
	Report(context.Context, incidents.Observation) (*incidents.Incident, incidents.Transition, error)
	Close(context.Context, string, string) error
	ActiveNodeIncidents(context.Context) ([]incidents.Incident, error)
}

type Config struct {
	Store     *store.Store
	Incidents IncidentReporter
	Logger    *slog.Logger
	Now       func() time.Time
	Retention time.Duration
}

type Service struct {
	store     *store.Store
	incidents IncidentReporter
	logger    *slog.Logger
	now       func() time.Time
	retention time.Duration
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
	retention := cfg.Retention
	if retention <= 0 {
		retention = HistoryRetention
	}
	return &Service{
		store:     cfg.Store,
		incidents: cfg.Incidents,
		logger:    logger,
		now:       now,
		retention: retention,
	}
}

func (s *Service) ready() error {
	if s == nil || s.store == nil || s.incidents == nil {
		return errors.New("monitoring: store and incidents are required")
	}
	return nil
}

// RecordHostPressure applies one accepted observation. It never persists a
// synthetic healthy sample: missing counters are reported as unknown so the
// incident lifecycle cannot treat them as recovery.
func (s *Service) RecordHostPressure(ctx context.Context, serverID uuid.UUID, nodeName string, obs servers.NodeObservation) {
	if s == nil || serverID == uuid.Nil || s.incidents == nil {
		return
	}
	rules, err := s.effectiveRules(ctx, serverID)
	if err != nil {
		s.logger.Warn("monitoring: resolve policy", "server_id", serverID.String(), "error", err)
		return
	}
	s.evaluateHost(ctx, serverID, nodeName, obs, rules)
	s.evaluateFilesystems(ctx, serverID, nodeName, obs, rules)
}

// GetSettings returns the effective rules plus whether the selected scope has
// its own saved policy.
func (s *Service) GetSettings(ctx context.Context, nodeID *uuid.UUID) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{}, errors.New("monitoring: store is not configured")
	}
	defaults := Defaults()
	instance, instanceSaved, instanceUpdated, err := s.loadInstancePolicy(ctx)
	if err != nil {
		return Settings{}, err
	}
	out := Settings{Defaults: defaults, Rules: instance, Effective: instance, UpdatedAt: instanceUpdated}
	if instanceSaved {
		out.Source = "instance"
	} else {
		out.Source = "default"
	}
	if nodeID == nil || *nodeID == uuid.Nil {
		return out, nil
	}
	node, found, updated, err := s.loadNodePolicy(ctx, *nodeID)
	if err != nil {
		return Settings{}, err
	}
	out.NodeID = nodeID
	out.HasOverride = found
	out.Effective = instance
	if found {
		out.Rules = node
		out.Source = "node"
		out.UpdatedAt = updated
		out.Effective = node
	}
	return out, nil
}

// SaveSettings validates and persists one scope. Rules apply on the next
// observation; changing them never evaluates history or fabricates a recovery.
func (s *Service) SaveSettings(ctx context.Context, userID uuid.UUID, nodeID *uuid.UUID, rules Rules) (Settings, error) {
	if s == nil || s.store == nil {
		return Settings{}, errors.New("monitoring: store is not configured")
	}
	if err := rules.Validate(); err != nil {
		return Settings{}, err
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return Settings{}, fmt.Errorf("monitoring: encode policy: %w", err)
	}
	now := pgtype.Timestamptz{Time: s.now().UTC(), Valid: true}
	if nodeID == nil || *nodeID == uuid.Nil {
		_, err = s.store.UpsertInstanceMonitoringPolicy(ctx, sqlc.UpsertInstanceMonitoringPolicyParams{
			ID:        pgUUID(uuid.New()),
			Rules:     raw,
			UpdatedBy: pgUUID(userID),
			UpdatedAt: now,
		})
	} else {
		if _, lookupErr := s.store.GetServerByID(ctx, pgUUID(*nodeID)); lookupErr != nil {
			if errors.Is(lookupErr, pgx.ErrNoRows) {
				return Settings{}, fmt.Errorf("%w: node does not exist", ErrValidation)
			}
			return Settings{}, fmt.Errorf("monitoring: load node: %w", lookupErr)
		}
		_, err = s.store.UpsertNodeMonitoringPolicy(ctx, sqlc.UpsertNodeMonitoringPolicyParams{
			ID:        pgUUID(uuid.New()),
			NodeID:    pgUUID(*nodeID),
			Rules:     raw,
			UpdatedBy: pgUUID(userID),
			UpdatedAt: now,
		})
	}
	if err != nil {
		return Settings{}, fmt.Errorf("monitoring: save policy: %w", err)
	}
	return s.GetSettings(ctx, nodeID)
}

// ResetSettings removes the selected scope's policy so the next scope applies.
func (s *Service) ResetSettings(ctx context.Context, nodeID *uuid.UUID) error {
	if s == nil || s.store == nil {
		return errors.New("monitoring: store is not configured")
	}
	var err error
	if nodeID == nil || *nodeID == uuid.Nil {
		_, err = s.store.DeleteInstanceMonitoringPolicy(ctx)
	} else {
		_, err = s.store.DeleteNodeMonitoringPolicy(ctx, pgUUID(*nodeID))
	}
	if err != nil {
		return fmt.Errorf("monitoring: reset policy: %w", err)
	}
	return nil
}

func (s *Service) effectiveRules(ctx context.Context, nodeID uuid.UUID) (Rules, error) {
	if nodeID != uuid.Nil {
		rules, found, _, err := s.loadNodePolicy(ctx, nodeID)
		if err != nil {
			return Rules{}, err
		}
		if found {
			return rules, nil
		}
	}
	rules, _, _, err := s.loadInstancePolicy(ctx)
	return rules, err
}

func (s *Service) loadInstancePolicy(ctx context.Context) (Rules, bool, *time.Time, error) {
	row, err := s.store.GetInstanceMonitoringPolicy(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Defaults(), false, nil, nil
		}
		return Rules{}, false, nil, fmt.Errorf("monitoring: load instance policy: %w", err)
	}
	rules, err := decodeRules(row.Rules)
	if err != nil {
		return Rules{}, false, nil, err
	}
	updated := row.UpdatedAt.Time.UTC()
	return rules, true, &updated, nil
}

func (s *Service) loadNodePolicy(ctx context.Context, nodeID uuid.UUID) (Rules, bool, *time.Time, error) {
	row, err := s.store.GetNodeMonitoringPolicy(ctx, pgUUID(nodeID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Rules{}, false, nil, nil
		}
		return Rules{}, false, nil, fmt.Errorf("monitoring: load node policy: %w", err)
	}
	rules, err := decodeRules(row.Rules)
	if err != nil {
		return Rules{}, false, nil, err
	}
	updated := row.UpdatedAt.Time.UTC()
	return rules, true, &updated, nil
}

// Sweep keeps missing evidence from counting as recovery and prunes expired
// history. Active incidents are owned by internal/incidents and never pruned
// here.
func (s *Service) Sweep(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	now := s.now().UTC()
	if err := s.markStale(ctx, now); err != nil {
		return err
	}
	if _, err := s.store.DeleteFilesystemUsageBefore(ctx, pgtype.Timestamptz{Time: now.Add(-s.retention), Valid: true}); err != nil {
		return fmt.Errorf("monitoring: prune filesystem history: %w", err)
	}
	return nil
}

// Run drives the staleness and retention sweeps until ctx ends.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := s.Sweep(sweepCtx); err != nil && ctx.Err() == nil {
				s.logger.Warn("monitoring: sweep", "error", err)
			}
			cancel()
		}
	}
}

func (s *Service) markStale(ctx context.Context, now time.Time) error {
	rows, err := s.incidents.ActiveNodeIncidents(ctx)
	if err != nil {
		return fmt.Errorf("monitoring: list active incidents: %w", err)
	}
	fresh := map[uuid.UUID]bool{}
	for _, inc := range rows {
		if !isPressureDetector(inc.Detector) || inc.NodeID == uuid.Nil {
			continue
		}
		if _, checked := fresh[inc.NodeID]; !checked {
			fresh[inc.NodeID] = s.nodeEvidenceFresh(ctx, inc.NodeID, now)
		}
		if fresh[inc.NodeID] {
			continue
		}
		_, _, err := s.incidents.Report(ctx, incidents.Observation{
			Identity:     inc.Identity,
			Detector:     inc.Detector,
			ScopeKind:    incidents.ScopeNode,
			ScopeID:      inc.NodeID,
			NodeID:       inc.NodeID,
			ResourceType: inc.ResourceType,
			ResourceID:   inc.ResourceID,
			ResourceName: inc.ResourceName,
			At:           now,
			Valid:        false,
			Evidence:     "no fresh observation",
		})
		if err != nil {
			return fmt.Errorf("monitoring: mark stale incident: %w", err)
		}
	}
	return nil
}

func (s *Service) nodeEvidenceFresh(ctx context.Context, nodeID uuid.UUID, now time.Time) bool {
	row, err := s.store.GetNodeObservation(ctx, pgUUID(nodeID))
	if err != nil {
		return false
	}
	interval := time.Duration(row.SamplingIntervalMs) * time.Millisecond
	window := 3 * interval
	if window <= 0 || window > staleAfter {
		window = staleAfter
	}
	age := now.Sub(row.ObservedAt.Time.UTC())
	return age >= 0 && age <= window
}

func isPressureDetector(detector string) bool {
	switch detector {
	case DetectorCPU, DetectorMemory, DetectorSpace, DetectorInodes:
		return true
	default:
		return false
	}
}

type Settings struct {
	NodeID      *uuid.UUID `json:"node_id,omitempty"`
	Rules       Rules      `json:"rules"`
	Effective   Rules      `json:"effective"`
	Defaults    Rules      `json:"defaults"`
	Source      string     `json:"source"`
	HasOverride bool       `json:"has_override"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
