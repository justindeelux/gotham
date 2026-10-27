-- +goose Up
-- Latest accepted node health observation (GOTHAM-82). One row per node keeps
-- only the newest accepted generation: host metrics, filesystem capacity and
-- managed container inventory. History is deliberately not stored here; this
-- is the evidence consumers read for current monitoring.
--
-- observed_at is the agent's collection time and received_at is the control
-- plane's receipt time, so freshness is re-evaluated after a restart instead of
-- restoring a row as newly observed. inventory_observed_at lets an accepted
-- partial report update host/filesystem evidence without replacing the last
-- complete inventory generation.
CREATE TABLE node_observations (
    server_id uuid PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    session_id text NOT NULL,
    sequence bigint NOT NULL,
    observed_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    sampling_interval_ms bigint NOT NULL,
    agent_version text NOT NULL DEFAULT '',
    host jsonb NOT NULL,
    filesystems jsonb NOT NULL,
    inventory jsonb NOT NULL,
    inventory_complete boolean NOT NULL DEFAULT false,
    inventory_reason text NOT NULL DEFAULT '',
    inventory_observed_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Freshness and retention reads scan by observation time.
CREATE INDEX node_observations_observed_at_idx ON node_observations (observed_at);

-- +goose Down
DROP TABLE IF EXISTS node_observations;
