-- +goose Up
-- Runtime health monitoring (GOTHAM-85). Container evidence arrives through
-- the node observation push (GOTHAM-82); this schema keeps the detector's
-- small amount of state and the per-resource monitoring settings. It never
-- stores logs, environment variables or arbitrary labels.
--
-- Settings are separate from applications/databases so a resource without
-- changed settings is monitored by default without a backfill. Replica
-- identity is application/database + Compose service + container number; the
-- container ID is evidence kept on the row, never part of the key.
CREATE TABLE runtime_monitor_settings (
    resource_type text NOT NULL CHECK (resource_type IN ('application', 'database')),
    resource_id uuid NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    grace_seconds integer NOT NULL DEFAULT 120
        CHECK (grace_seconds >= 30 AND grace_seconds <= 600),
    -- desired_state is the last explicit user intent for an application.
    -- A stopped resource is an intentional stop, not a failure.
    desired_state text NOT NULL DEFAULT 'running'
        CHECK (desired_state IN ('running', 'stopped')),
    -- grace_until bounds the startup grace after a start action; deployments
    -- use their own finished_at instead.
    grace_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (resource_type, resource_id)
);

CREATE TABLE runtime_monitor_replicas (
    node_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    resource_type text NOT NULL CHECK (resource_type IN ('application', 'database')),
    resource_id uuid NOT NULL,
    service text NOT NULL DEFAULT '',
    replica text NOT NULL DEFAULT '',
    container_id text NOT NULL DEFAULT '',
    deployment_id text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'unknown',
    health text NOT NULL DEFAULT 'unknown',
    -- reason is the detector's short machine-readable verdict for the row.
    reason text NOT NULL DEFAULT '',
    -- coverage is complete or unknown for this replica's last evaluation.
    coverage text NOT NULL DEFAULT 'unknown',
    restart_count bigint NOT NULL DEFAULT 0,
    -- restart_events holds the restart increases seen inside the rolling
    -- five-minute window as an array of RFC3339 timestamps.
    restart_events jsonb NOT NULL DEFAULT '[]',
    oom_container text NOT NULL DEFAULT '',
    oom_at timestamptz,
    observed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_id, resource_type, resource_id, service, replica)
);

CREATE INDEX runtime_monitor_replicas_resource_idx
    ON runtime_monitor_replicas (resource_type, resource_id);
CREATE INDEX runtime_monitor_replicas_node_idx
    ON runtime_monitor_replicas (node_id);

-- +goose Down
DROP TABLE IF EXISTS runtime_monitor_replicas;
DROP TABLE IF EXISTS runtime_monitor_settings;
