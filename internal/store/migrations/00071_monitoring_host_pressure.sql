-- +goose Up
-- Host resource pressure policies (GOTHAM-84). One instance row holds the
-- product defaults; an optional node row overrides them. Rules are stored as
-- JSON so the evaluator owns the shape while PostgreSQL keeps the audit trail.
ALTER TABLE incidents ADD COLUMN severity_since timestamptz;

CREATE TABLE monitoring_policies (
    id uuid PRIMARY KEY,
    scope text NOT NULL,
    node_id uuid REFERENCES servers(id) ON DELETE CASCADE,
    rules jsonb NOT NULL,
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT monitoring_policies_scope_check CHECK (scope IN ('instance', 'node')),
    CONSTRAINT monitoring_policies_node_check CHECK (
        (scope = 'instance' AND node_id IS NULL)
        OR (scope = 'node' AND node_id IS NOT NULL)
    )
);
CREATE UNIQUE INDEX monitoring_policies_instance_idx
    ON monitoring_policies ((scope)) WHERE scope = 'instance';
CREATE UNIQUE INDEX monitoring_policies_node_idx
    ON monitoring_policies (node_id) WHERE scope = 'node';

-- Stable node/filesystem identity. segment advances when the reported capacity
-- changes (resize or replacement), so history and later forecasting never cross
-- a discontinuity. A removed filesystem keeps its rows until retention prunes
-- the buckets.
CREATE TABLE filesystem_identities (
    id uuid PRIMARY KEY,
    node_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    filesystem_key text NOT NULL,
    path text NOT NULL,
    purpose text NOT NULL DEFAULT '',
    segment integer NOT NULL DEFAULT 1,
    total_bytes bigint NOT NULL DEFAULT 0,
    total_inodes bigint NOT NULL DEFAULT 0,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    UNIQUE (node_id, filesystem_key)
);

-- Hourly aggregates instead of raw 15-second samples: count/min/max plus the
-- quality split keeps 30 days of history small and never invents a zero sample
-- for an unreadable counter.
CREATE TABLE filesystem_usage_buckets (
    identity_id uuid NOT NULL REFERENCES filesystem_identities(id) ON DELETE CASCADE,
    segment integer NOT NULL,
    bucket_at timestamptz NOT NULL,
    samples integer NOT NULL DEFAULT 0,
    fresh_samples integer NOT NULL DEFAULT 0,
    partial_samples integer NOT NULL DEFAULT 0,
    unknown_samples integer NOT NULL DEFAULT 0,
    min_used_bytes bigint,
    max_used_bytes bigint,
    last_used_bytes bigint,
    min_used_inodes bigint,
    max_used_inodes bigint,
    last_used_inodes bigint,
    total_bytes bigint NOT NULL DEFAULT 0,
    total_inodes bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (identity_id, segment, bucket_at)
);
CREATE INDEX filesystem_usage_buckets_retention_idx
    ON filesystem_usage_buckets (bucket_at);

-- +goose Down
DROP TABLE IF EXISTS filesystem_usage_buckets;
DROP TABLE IF EXISTS filesystem_identities;
DROP TABLE IF EXISTS monitoring_policies;
ALTER TABLE incidents DROP COLUMN IF EXISTS severity_since;
