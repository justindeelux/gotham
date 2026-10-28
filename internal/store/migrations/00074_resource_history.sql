-- +goose Up
-- Application/container resource history (GOTHAM-90). Raw samples arrive with
-- the node observation push and are retained for 24 hours. Minute rollups cover
-- seven days and hour rollups cover thirty days. A nullable metric is unknown,
-- never a measured zero; gaps stay gaps.
--
-- The stable identity is resource + Compose service, and deployment_id marks
-- the container incarnation so a replacement never mixes with the old one.
-- Container IDs and server IDs are evidence for ingest, never part of a chart
-- identity.
CREATE TABLE resource_samples (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    resource_type text NOT NULL CHECK (resource_type IN ('application', 'database')),
    resource_id uuid NOT NULL,
    -- The typed references make deletion cascade to the resource's history.
    application_id uuid REFERENCES applications(id) ON DELETE CASCADE,
    database_id uuid REFERENCES databases(id) ON DELETE CASCADE,
    service text NOT NULL DEFAULT '',
    deployment_id text NOT NULL DEFAULT '',
    replica text NOT NULL DEFAULT '',
    container_id text NOT NULL DEFAULT '',
    observed_at timestamptz NOT NULL,
    cpu_cores double precision CHECK (cpu_cores IS NULL OR cpu_cores >= 0),
    mem_working_set_bytes bigint CHECK (mem_working_set_bytes IS NULL OR mem_working_set_bytes >= 0),
    mem_limit_bytes bigint CHECK (mem_limit_bytes IS NULL OR mem_limit_bytes >= 0),
    mem_unlimited boolean NOT NULL DEFAULT false,
    net_rx_bps double precision CHECK (net_rx_bps IS NULL OR net_rx_bps >= 0),
    net_tx_bps double precision CHECK (net_tx_bps IS NULL OR net_tx_bps >= 0),
    disk_read_bps double precision CHECK (disk_read_bps IS NULL OR disk_read_bps >= 0),
    disk_write_bps double precision CHECK (disk_write_bps IS NULL OR disk_write_bps >= 0),
    expected_replicas integer NOT NULL DEFAULT 0 CHECK (expected_replicas >= 0),
    observed_replicas integer NOT NULL DEFAULT 0 CHECK (observed_replicas >= 0),
    coverage_partial boolean NOT NULL DEFAULT false,
    UNIQUE (server_id, container_id, observed_at)
);

CREATE INDEX resource_samples_resource_time_idx
    ON resource_samples (resource_type, resource_id, observed_at);
CREATE INDEX resource_samples_observed_at_idx ON resource_samples (observed_at);
CREATE INDEX resource_samples_application_idx ON resource_samples (application_id);
CREATE INDEX resource_samples_database_idx ON resource_samples (database_id);

CREATE TABLE resource_metric_rollups (
    tier text NOT NULL CHECK (tier IN ('minute', 'hour')),
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    application_id uuid REFERENCES applications(id) ON DELETE CASCADE,
    database_id uuid REFERENCES databases(id) ON DELETE CASCADE,
    service text NOT NULL DEFAULT '',
    deployment_id text NOT NULL DEFAULT '',
    bucket timestamptz NOT NULL,
    metric text NOT NULL CHECK (metric IN (
        'cpu_cores',
        'mem_working_set_bytes',
        'net_rx_bps',
        'net_tx_bps',
        'disk_read_bps',
        'disk_write_bps'
    )),
    samples bigint NOT NULL,
    replicas integer NOT NULL,
    expected_replicas integer NOT NULL,
    coverage_partial boolean NOT NULL DEFAULT false,
    value_sum double precision NOT NULL,
    value_min double precision NOT NULL,
    value_max double precision NOT NULL,
    -- latest_at is the newest observation behind this bucket, so the chart can
    -- show the observed time rather than the bucket edge.
    latest_at timestamptz NOT NULL,
    -- The memory limit is series metadata, not a metric: it is carried on each
    -- metric row so an unlimited container is never shown as a 0-byte limit.
    mem_limit_bytes bigint NOT NULL DEFAULT 0,
    mem_unlimited boolean NOT NULL DEFAULT false,
    PRIMARY KEY (tier, resource_type, resource_id, service, deployment_id, bucket, metric)
);

CREATE INDEX resource_metric_rollups_lookup_idx
    ON resource_metric_rollups (tier, resource_type, resource_id, bucket);
CREATE INDEX resource_metric_rollups_bucket_idx ON resource_metric_rollups (tier, bucket);

-- +goose Down
DROP TABLE IF EXISTS resource_metric_rollups;
DROP TABLE IF EXISTS resource_samples;
