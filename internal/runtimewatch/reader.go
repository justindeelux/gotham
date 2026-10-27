package runtimewatch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Health is the read-only runtime summary attached to application and
// database detail/list responses. It is derived from pushed evidence, never
// from a live Docker call.
type Health struct {
	State        string          `json:"state"`
	Coverage     string          `json:"coverage"`
	Reason       string          `json:"reason,omitempty"`
	ObservedAt   *time.Time      `json:"observed_at,omitempty"`
	Enabled      bool            `json:"enabled"`
	GraceSeconds int             `json:"grace_seconds"`
	IncidentID   string          `json:"incident_id,omitempty"`
	Affected     []ReplicaHealth `json:"affected"`
}

type ReplicaHealth struct {
	Service       string     `json:"service,omitempty"`
	Replica       string     `json:"replica,omitempty"`
	ContainerID   string     `json:"container_id,omitempty"`
	DeploymentID  string     `json:"deployment_id,omitempty"`
	State         string     `json:"state"`
	Health        string     `json:"health"`
	Reason        string     `json:"reason,omitempty"`
	Coverage      string     `json:"coverage"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
	LastRestartAt *time.Time `json:"last_restart_at,omitempty"`
	LastOOMAt     *time.Time `json:"last_oom_at,omitempty"`
}

type Reader struct {
	st  *store.Store
	now func() time.Time
}

func NewReader(st *store.Store) *Reader {
	return &Reader{st: st, now: time.Now}
}

func (r *Reader) ApplicationHealth(ctx context.Context, id uuid.UUID) Health {
	return r.health(ctx, ResourceApplication, []uuid.UUID{id})[id]
}

func (r *Reader) DatabaseHealth(ctx context.Context, id uuid.UUID) Health {
	return r.health(ctx, ResourceDatabase, []uuid.UUID{id})[id]
}

func (r *Reader) ApplicationHealthMany(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]Health {
	return r.health(ctx, ResourceApplication, ids)
}

func (r *Reader) DatabaseHealthMany(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]Health {
	return r.health(ctx, ResourceDatabase, ids)
}

func (r *Reader) health(ctx context.Context, resourceType string, ids []uuid.UUID) map[uuid.UUID]Health {
	out := make(map[uuid.UUID]Health, len(ids))
	if r == nil || r.st == nil || len(ids) == 0 {
		return out
	}
	wanted := make(map[uuid.UUID]bool, len(ids))
	pgIDs := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || wanted[id] {
			continue
		}
		wanted[id] = true
		pgIDs = append(pgIDs, pgUUID(id))
	}
	if len(pgIDs) == 0 {
		return out
	}
	rows, err := r.st.ListRuntimeMonitorReplicaStatesByResources(ctx, sqlc.ListRuntimeMonitorReplicaStatesByResourcesParams{
		ResourceType: resourceType,
		ResourceIds:  pgIDs,
	})
	if err != nil {
		return out
	}
	states := map[uuid.UUID][]sqlc.RuntimeMonitorReplica{}
	for _, row := range rows {
		states[row.ResourceID.Bytes] = append(states[row.ResourceID.Bytes], row)
	}
	incidentsByResource := map[uuid.UUID]uuid.UUID{}
	if active, err := r.st.ListActiveIncidentsByResources(ctx, sqlc.ListActiveIncidentsByResourcesParams{
		ResourceType: resourceType,
		ResourceIds:  pgIDs,
	}); err == nil {
		for _, row := range active {
			if row.ResourceID.Valid && incidentsByResource[row.ResourceID.Bytes] == uuid.Nil {
				incidentsByResource[row.ResourceID.Bytes] = row.ID.Bytes
			}
		}
	}
	now := r.now().UTC()
	for _, id := range ids {
		if !wanted[id] {
			continue
		}
		settings := DefaultSettings()
		if row, err := r.st.GetRuntimeMonitorSettings(ctx, sqlc.GetRuntimeMonitorSettingsParams{
			ResourceType: resourceType,
			ResourceID:   pgUUID(id),
		}); err == nil {
			settings.Enabled = row.Enabled
			settings.Grace = time.Duration(row.GraceSeconds) * time.Second
			settings.DesiredState = row.DesiredState
		} else if !errors.Is(err, pgx.ErrNoRows) {
			// A read failure still returns defaults; the summary is advisory.
			settings = DefaultSettings()
		}
		out[id] = summarize(settings, states[id], incidentsByResource[id], now)
	}
	return out
}

func summarize(settings Settings, rows []sqlc.RuntimeMonitorReplica, incidentID uuid.UUID, now time.Time) Health {
	health := Health{
		State:        StateUnknown,
		Coverage:     CoverageUnknown,
		Enabled:      settings.Enabled,
		GraceSeconds: int(settings.Grace / time.Second),
		Affected:     make([]ReplicaHealth, 0, len(rows)),
	}
	if incidentID != uuid.Nil {
		health.IncidentID = incidentID.String()
	}
	if !settings.Enabled {
		health.State = StateDisabled
		health.Reason = "disabled"
	}
	var newest time.Time
	coverageKnown := len(rows) > 0
	for _, row := range rows {
		replica := ReplicaHealth{
			Service:      row.Service,
			Replica:      row.Replica,
			ContainerID:  row.ContainerID,
			DeploymentID: row.DeploymentID,
			State:        row.State,
			Health:       row.Health,
			Reason:       row.Reason,
			Coverage:     row.Coverage,
		}
		if row.ObservedAt.Valid {
			at := row.ObservedAt.Time.UTC()
			replica.ObservedAt = &at
			if at.After(newest) {
				newest = at
			}
		}
		if row.Coverage != CoverageComplete {
			coverageKnown = false
		}
		var events []time.Time
		if err := json.Unmarshal(row.RestartEvents, &events); err == nil && len(events) > 0 {
			last := events[0]
			for _, at := range events[1:] {
				if at.After(last) {
					last = at
				}
			}
			last = last.UTC()
			replica.LastRestartAt = &last
		}
		if row.OomAt.Valid {
			at := row.OomAt.Time.UTC()
			replica.LastOOMAt = &at
		}
		health.Affected = append(health.Affected, replica)
	}
	if !newest.IsZero() {
		at := newest
		health.ObservedAt = &at
	}
	if !settings.Enabled {
		return health
	}
	if len(rows) == 0 {
		health.Reason = "not_observed"
		return health
	}
	if !coverageKnown || newest.IsZero() || now.Sub(newest) > staleAfter {
		health.State = StateUnknown
		health.Coverage = CoverageUnknown
		health.Reason = "stale_observation"
		return health
	}
	health.Coverage = CoverageComplete
	health.State, health.Reason = aggregateRows(summaryRows(rows))
	return health
}

type replicaRow struct {
	State        string
	Health       string
	Reason       string
	ContainerID  string
	OOMContainer string
}

func summaryRows(rows []sqlc.RuntimeMonitorReplica) []replicaRow {
	out := make([]replicaRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, replicaRow{
			State:        row.State,
			Health:       row.Health,
			Reason:       row.Reason,
			ContainerID:  row.ContainerID,
			OOMContainer: row.OomContainer,
		})
	}
	return out
}

func aggregateRows(rows []replicaRow) (string, string) {
	state := StateHealthy
	reason := ""
	starting := false
	stopped := false
	stoppedReason := ""
	unknownHealth := false
	for _, row := range rows {
		switch {
		case row.State == "missing" || row.State == "paused":
			return StateUnavailable, row.Reason
		case row.OOMContainer != "" && row.ContainerID == row.OOMContainer && row.State == "stopped":
			// OOM is current only while the same container is still
			// terminated. A recreated or policy-restarted healthy container
			// keeps last_oom_at as evidence but no longer degrades the summary.
			return StateUnavailable, "oom_killed"
		case row.State == "stopped" && row.Reason != "one_shot_completed" && row.Reason != "intentionally_stopped":
			return StateUnavailable, row.Reason
		case row.State == "unhealthy":
			return StateDegraded, row.Reason
		case row.State == "starting":
			starting = true
		case row.State == "stopped":
			stopped = true
			if stoppedReason == "" || row.Reason == "intentionally_stopped" {
				stoppedReason = row.Reason
			}
		case row.State == "running":
			if row.Health != "healthy" {
				unknownHealth = true
				if reason == "" {
					reason = "no_healthcheck"
				}
			}
		default:
			unknownHealth = true
			if reason == "" {
				reason = "unknown_state"
			}
		}
	}
	if starting {
		return StateStarting, "starting"
	}
	if state == StateDegraded {
		return state, reason
	}
	if stopped {
		if stoppedReason != "" {
			return StateStopped, stoppedReason
		}
		return StateStopped, "one_shot_completed"
	}
	if unknownHealth {
		return StateUnknown, reason
	}
	return state, reason
}
