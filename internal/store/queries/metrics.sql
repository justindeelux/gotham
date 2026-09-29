-- Server time-series queries (Phase 8, BE-8.4).

-- name: InsertServerMetric :exec
-- Appends one heartbeat sample. A duplicate (server, instant) is ignored: the
-- heartbeat stream may resend after a transport hiccup, and the point already
-- stored is the same observation.
INSERT INTO server_metrics (
    server_id,
    recorded_at,
    cpu_usage,
    mem_usage,
    disk_usage,
    net_rx_bps,
    net_tx_bps,
    disk_read_bps,
    disk_write_bps,
    container_count
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (server_id, recorded_at) DO NOTHING;

-- name: ListServerMetrics :many
-- One row per non-empty aggregation bucket, ordered oldest first. Buckets
-- without samples are omitted rather than returned as zero/NaN points, so the
-- chart draws gaps (or connects across them) instead of inventing data.
SELECT
    date_bin(@bucket::interval, recorded_at, 'epoch'::timestamptz)::timestamptz AS bucket,
    avg(cpu_usage)::float8 AS cpu_usage,
    avg(mem_usage)::float8 AS mem_usage,
    avg(disk_usage)::float8 AS disk_usage,
    avg(net_rx_bps)::float8 AS net_rx_bps,
    avg(net_tx_bps)::float8 AS net_tx_bps,
    avg(disk_read_bps)::float8 AS disk_read_bps,
    avg(disk_write_bps)::float8 AS disk_write_bps,
    avg(container_count)::float8 AS container_count
FROM server_metrics
WHERE server_id = $1
  AND recorded_at >= sqlc.arg(range_start)
  AND recorded_at < sqlc.arg(range_end)
GROUP BY bucket
ORDER BY bucket;

-- name: DeleteServerMetricsBefore :execrows
-- Retention sweep: drops samples recorded before the cutoff. Runs on its own
-- schedule, never per heartbeat.
DELETE FROM server_metrics WHERE recorded_at < $1;
