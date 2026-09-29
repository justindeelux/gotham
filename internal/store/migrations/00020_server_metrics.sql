-- +goose Up
-- Host time series (Phase 8, BE-8.4). Every heartbeat appends one sample here
-- while the servers row keeps only the latest snapshot, so charts can read a
-- range without touching the hot registry row.
--
-- The primary key is (server_id, recorded_at) with recorded_at taken from the
-- heartbeat's sent_at: a retried or duplicated heartbeat for the same instant
-- is deduplicated by ON CONFLICT DO NOTHING instead of erroring or storing the
-- same point twice.
--
-- All metric columns are NOT NULL because proto3 cannot distinguish "unset"
-- from zero: a node whose platform cannot report a metric reports zero, and
-- zero is a legitimate rate. Rows cascade with their server, and retention
-- (30 days) is swept by the control plane's periodic sweep.
CREATE TABLE server_metrics (
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    recorded_at timestamptz NOT NULL,
    cpu_usage double precision NOT NULL,
    mem_usage double precision NOT NULL,
    disk_usage double precision NOT NULL,
    net_rx_bps double precision NOT NULL,
    net_tx_bps double precision NOT NULL,
    disk_read_bps double precision NOT NULL,
    disk_write_bps double precision NOT NULL,
    container_count bigint NOT NULL,
    PRIMARY KEY (server_id, recorded_at)
);

-- The range query filters by server and time; the descending order serves the
-- newest-first reads the charts open with.
CREATE INDEX server_metrics_server_time_idx ON server_metrics (server_id, recorded_at DESC);

-- +goose Down
DROP TABLE IF EXISTS server_metrics;
