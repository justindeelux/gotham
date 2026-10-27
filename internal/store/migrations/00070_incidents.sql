-- +goose Up
-- Persistent incident lifecycle (GOTHAM-83). A notification is a delivered
-- event; an incident is a condition that is still actionable. One row tracks
-- one occurrence from pending through firing to resolved, so a full
-- notification queue or a control-plane restart cannot erase an active
-- problem.
--
-- identity_key is the stable detector identity (application/database IDs plus
-- service or filesystem identity); a container ID is only evidence and never
-- part of the key. The partial unique index keeps at most one open occurrence
-- per identity; a recurrence after resolution creates a new row so history
-- stays readable.
CREATE TABLE incidents (
    id uuid PRIMARY KEY,
    identity_key text NOT NULL,
    detector_key text NOT NULL,
    scope_kind text NOT NULL,
    scope_id uuid,
    node_id uuid REFERENCES servers(id) ON DELETE SET NULL,
    project_id uuid REFERENCES projects(id) ON DELETE SET NULL,
    resource_type text NOT NULL DEFAULT '',
    resource_id uuid,
    resource_name text NOT NULL DEFAULT '',
    severity text NOT NULL,
    lifecycle text NOT NULL,
    coverage text NOT NULL DEFAULT 'unknown',
    first_observed_at timestamptz NOT NULL,
    last_observed_at timestamptz NOT NULL,
    last_valid_at timestamptz,
    confirmed_at timestamptz,
    recovery_started_at timestamptz,
    resolved_at timestamptz,
    close_reason text NOT NULL DEFAULT '',
    acknowledged_at timestamptz,
    acknowledged_by uuid REFERENCES users(id) ON DELETE SET NULL,
    snoozed_until timestamptz,
    snooze_notified_until timestamptz,
    delivery_state text NOT NULL DEFAULT 'none',
    delivery_hold text NOT NULL DEFAULT '',
    delivery_event text NOT NULL DEFAULT '',
    delivery_attempts integer NOT NULL DEFAULT 0,
    delivery_error text NOT NULL DEFAULT '',
    last_notified_severity text NOT NULL DEFAULT '',
    evidence text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX incidents_open_identity_idx ON incidents (identity_key)
    WHERE lifecycle <> 'resolved';
CREATE INDEX incidents_open_updated_idx ON incidents (updated_at DESC, id DESC)
    WHERE lifecycle <> 'resolved';
CREATE INDEX incidents_resolved_idx ON incidents (resolved_at)
    WHERE lifecycle = 'resolved';
CREATE INDEX incidents_project_idx ON incidents (project_id, updated_at DESC)
    WHERE lifecycle <> 'resolved';
CREATE INDEX incidents_node_idx ON incidents (node_id)
    WHERE lifecycle <> 'resolved';

-- Transition history: append-only, bounded per incident by the repository.
CREATE TABLE incident_events (
    id uuid PRIMARY KEY,
    incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    kind text NOT NULL,
    from_lifecycle text NOT NULL DEFAULT '',
    to_lifecycle text NOT NULL DEFAULT '',
    from_severity text NOT NULL DEFAULT '',
    to_severity text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT '',
    actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
    detail text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL
);
CREATE INDEX incident_events_incident_idx ON incident_events (incident_id, created_at DESC, id DESC);

-- Local delivery intent committed in the same transaction as the transition.
-- The notifications layer performs channel selection and rendering; a failed
-- handoff stays here as retryable rather than being discarded.
CREATE TABLE incident_delivery_intents (
    id uuid PRIMARY KEY,
    incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    transition text NOT NULL,
    severity text NOT NULL,
    state text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX incident_delivery_intents_pending_idx ON incident_delivery_intents (created_at, id)
    WHERE state = 'pending';
CREATE INDEX incident_delivery_intents_incident_idx ON incident_delivery_intents (incident_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS incident_delivery_intents;
DROP TABLE IF EXISTS incident_events;
DROP TABLE IF EXISTS incidents;
