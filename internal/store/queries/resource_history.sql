-- Application resource history queries (GOTHAM-90). Raw samples live for 24h;
-- minute and hour rollups carry count/min/max/sum plus replica coverage so a
-- chart never turns a gap or a partial replica set into a healthy zero.

-- name: InsertResourceSamples :copyfrom
-- Batched ingest: one observation generation writes all its container samples
-- in one COPY instead of one round trip per container.
INSERT INTO resource_samples (
    server_id,
    resource_type,
    resource_id,
    application_id,
    database_id,
    service,
    deployment_id,
    replica,
    container_id,
    observed_at,
    cpu_cores,
    mem_working_set_bytes,
    mem_limit_bytes,
    mem_unlimited,
    net_rx_bps,
    net_tx_bps,
    disk_read_bps,
    disk_write_bps,
    expected_replicas,
    observed_replicas,
    coverage_partial
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
    $17, $18, $19, $20, $21
);

-- name: ListResourceSamples :many
-- The one-hour range reads raw rows and aggregates them in Go, so a metric that
-- was unknown in a sample stays absent from that bucket instead of becoming a
-- zero. One hour at the 30-second cadence is a bounded row count.
SELECT
    observed_at,
    service,
    deployment_id,
    replica,
    cpu_cores,
    mem_working_set_bytes,
    mem_limit_bytes,
    mem_unlimited,
    net_rx_bps,
    net_tx_bps,
    disk_read_bps,
    disk_write_bps,
    expected_replicas,
    coverage_partial
FROM resource_samples
WHERE resource_type = $1
  AND resource_id = $2
  AND observed_at >= @range_start
  AND observed_at < @range_end
  AND (sqlc.narg(service_filter)::text IS NULL OR service = sqlc.narg(service_filter))
ORDER BY observed_at;

-- name: ListResourceRollupPoints :many
SELECT
    bucket,
    service,
    deployment_id,
    metric,
    samples,
    replicas,
    expected_replicas,
    coverage_partial,
    value_sum,
    value_min,
    value_max,
    latest_at,
    mem_limit_bytes,
    mem_unlimited
FROM resource_metric_rollups
WHERE tier = @tier
  AND resource_type = $1
  AND resource_id = $2
  AND bucket >= @range_start
  AND bucket < @range_end
  AND (sqlc.narg(service_filter)::text IS NULL OR service = sqlc.narg(service_filter))
ORDER BY bucket, service, deployment_id, metric;

-- name: UpsertResourceMinuteRollups :execrows
-- Recomputes the bounded recent window from raw samples. Upserts are
-- idempotent, so a sweep may overlap the previous one without double counting.
WITH points AS (
    SELECT rs.resource_type, rs.resource_id, rs.application_id, rs.database_id, rs.service,
           rs.deployment_id, rs.replica, rs.observed_at, rs.expected_replicas, rs.coverage_partial,
           rs.mem_limit_bytes, rs.mem_unlimited,
           'cpu_cores'::text AS metric, rs.cpu_cores::float8 AS value
    FROM resource_samples rs
    WHERE rs.observed_at >= @range_start AND rs.observed_at < @range_end AND rs.cpu_cores IS NOT NULL
    UNION ALL
    SELECT rs.resource_type, rs.resource_id, rs.application_id, rs.database_id, rs.service,
           rs.deployment_id, rs.replica, rs.observed_at, rs.expected_replicas, rs.coverage_partial,
           rs.mem_limit_bytes, rs.mem_unlimited,
           'mem_working_set_bytes'::text, rs.mem_working_set_bytes::float8
    FROM resource_samples rs
    WHERE rs.observed_at >= @range_start AND rs.observed_at < @range_end AND rs.mem_working_set_bytes IS NOT NULL
    UNION ALL
    SELECT rs.resource_type, rs.resource_id, rs.application_id, rs.database_id, rs.service,
           rs.deployment_id, rs.replica, rs.observed_at, rs.expected_replicas, rs.coverage_partial,
           rs.mem_limit_bytes, rs.mem_unlimited,
           'net_rx_bps'::text, rs.net_rx_bps::float8
    FROM resource_samples rs
    WHERE rs.observed_at >= @range_start AND rs.observed_at < @range_end AND rs.net_rx_bps IS NOT NULL
    UNION ALL
    SELECT rs.resource_type, rs.resource_id, rs.application_id, rs.database_id, rs.service,
           rs.deployment_id, rs.replica, rs.observed_at, rs.expected_replicas, rs.coverage_partial,
           rs.mem_limit_bytes, rs.mem_unlimited,
           'net_tx_bps'::text, rs.net_tx_bps::float8
    FROM resource_samples rs
    WHERE rs.observed_at >= @range_start AND rs.observed_at < @range_end AND rs.net_tx_bps IS NOT NULL
    UNION ALL
    SELECT rs.resource_type, rs.resource_id, rs.application_id, rs.database_id, rs.service,
           rs.deployment_id, rs.replica, rs.observed_at, rs.expected_replicas, rs.coverage_partial,
           rs.mem_limit_bytes, rs.mem_unlimited,
           'disk_read_bps'::text, rs.disk_read_bps::float8
    FROM resource_samples rs
    WHERE rs.observed_at >= @range_start AND rs.observed_at < @range_end AND rs.disk_read_bps IS NOT NULL
    UNION ALL
    SELECT rs.resource_type, rs.resource_id, rs.application_id, rs.database_id, rs.service,
           rs.deployment_id, rs.replica, rs.observed_at, rs.expected_replicas, rs.coverage_partial,
           rs.mem_limit_bytes, rs.mem_unlimited,
           'disk_write_bps'::text, rs.disk_write_bps::float8
    FROM resource_samples rs
    WHERE rs.observed_at >= @range_start AND rs.observed_at < @range_end AND rs.disk_write_bps IS NOT NULL
)
INSERT INTO resource_metric_rollups (
    tier, resource_type, resource_id, application_id, database_id, service,
    deployment_id, bucket, metric, samples, replicas, expected_replicas,
    coverage_partial, value_sum, value_min, value_max, latest_at,
    mem_limit_bytes, mem_unlimited
)
SELECT
    'minute',
    p.resource_type,
    p.resource_id,
    p.application_id,
    p.database_id,
    p.service,
    p.deployment_id,
    date_bin('1 minute'::interval, p.observed_at, 'epoch'::timestamptz)::timestamptz,
    p.metric,
    count(*)::bigint,
    count(DISTINCT p.replica)::integer,
    max(p.expected_replicas)::integer,
    coalesce(bool_or(p.coverage_partial), false),
    sum(p.value)::float8,
    min(p.value)::float8,
    max(p.value)::float8,
    max(p.observed_at)::timestamptz,
    coalesce(max(p.mem_limit_bytes), 0)::bigint,
    coalesce(bool_or(p.mem_unlimited), false)
FROM points p
GROUP BY p.resource_type, p.resource_id, p.application_id, p.database_id, p.service,
         p.deployment_id, p.metric,
         date_bin('1 minute'::interval, p.observed_at, 'epoch'::timestamptz)
ON CONFLICT (tier, resource_type, resource_id, service, deployment_id, bucket, metric)
DO UPDATE SET
    application_id = excluded.application_id,
    database_id = excluded.database_id,
    samples = excluded.samples,
    replicas = excluded.replicas,
    expected_replicas = excluded.expected_replicas,
    coverage_partial = excluded.coverage_partial,
    value_sum = excluded.value_sum,
    value_min = excluded.value_min,
    value_max = excluded.value_max,
    latest_at = excluded.latest_at,
    mem_limit_bytes = excluded.mem_limit_bytes,
    mem_unlimited = excluded.mem_unlimited;

-- name: UpsertResourceHourRollups :execrows
-- Hour rollups are built from the minute tier so hourly retention never needs
-- raw samples older than 24 hours.
WITH points AS (
    SELECT r.resource_type, r.resource_id, r.application_id, r.database_id, r.service,
           r.deployment_id, r.bucket, r.metric, r.samples, r.replicas, r.expected_replicas,
           r.coverage_partial, r.value_sum, r.value_min, r.value_max, r.latest_at,
           r.mem_limit_bytes, r.mem_unlimited
    FROM resource_metric_rollups r
    WHERE r.tier = 'minute' AND r.bucket >= @range_start AND r.bucket < @range_end
)
INSERT INTO resource_metric_rollups (
    tier, resource_type, resource_id, application_id, database_id, service,
    deployment_id, bucket, metric, samples, replicas, expected_replicas,
    coverage_partial, value_sum, value_min, value_max, latest_at,
    mem_limit_bytes, mem_unlimited
)
SELECT
    'hour',
    p.resource_type,
    p.resource_id,
    p.application_id,
    p.database_id,
    p.service,
    p.deployment_id,
    date_bin('1 hour'::interval, p.bucket, 'epoch'::timestamptz)::timestamptz,
    p.metric,
    sum(p.samples)::bigint,
    max(p.replicas)::integer,
    max(p.expected_replicas)::integer,
    coalesce(bool_or(p.coverage_partial), false),
    sum(p.value_sum)::float8,
    min(p.value_min)::float8,
    max(p.value_max)::float8,
    max(p.latest_at)::timestamptz,
    coalesce(max(p.mem_limit_bytes), 0)::bigint,
    coalesce(bool_or(p.mem_unlimited), false)
FROM points p
GROUP BY p.resource_type, p.resource_id, p.application_id, p.database_id, p.service,
         p.deployment_id, p.metric,
         date_bin('1 hour'::interval, p.bucket, 'epoch'::timestamptz)
ON CONFLICT (tier, resource_type, resource_id, service, deployment_id, bucket, metric)
DO UPDATE SET
    application_id = excluded.application_id,
    database_id = excluded.database_id,
    samples = excluded.samples,
    replicas = excluded.replicas,
    expected_replicas = excluded.expected_replicas,
    coverage_partial = excluded.coverage_partial,
    value_sum = excluded.value_sum,
    value_min = excluded.value_min,
    value_max = excluded.value_max,
    latest_at = excluded.latest_at,
    mem_limit_bytes = excluded.mem_limit_bytes,
    mem_unlimited = excluded.mem_unlimited;

-- name: DeleteResourceSamplesBefore :execrows
DELETE FROM resource_samples WHERE observed_at < $1;

-- name: DeleteResourceRollupsBefore :execrows
DELETE FROM resource_metric_rollups WHERE tier = $1 AND bucket < $2;

-- name: DeleteResourceHistory :execrows
DELETE FROM resource_samples WHERE resource_type = $1 AND resource_id = $2;

-- name: DeleteResourceRollupHistory :execrows
DELETE FROM resource_metric_rollups WHERE resource_type = $1 AND resource_id = $2;

-- name: ResourceHistoryExists :one
SELECT EXISTS (
    SELECT 1 FROM resource_samples rs WHERE rs.resource_type = $1 AND rs.resource_id = $2
    UNION ALL
    SELECT 1 FROM resource_metric_rollups rr WHERE rr.resource_type = $1 AND rr.resource_id = $2
) AS exists;
