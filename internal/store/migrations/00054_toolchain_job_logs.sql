-- +goose Up
-- Toolchain queue round 1 (PR #325 review). Two changes:
--
-- 1. The job log moves from one inline column on toolchain_jobs into an
--    append-only child table. A line-per-line `log = log || $line` rewrote (and
--    re-TOASTed) the whole value on every append, so a chatty install cost
--    O(n^2) bytes; one small row per line is a true append and reads back by
--    (job_id, seq) for incremental fetches.
-- 2. A running job records which instance claimed it and a lease that the
--    worker renews, so boot recovery can fail only this instance's jobs or jobs
--    whose lease lapsed instead of every running job (multi-instance safe).
ALTER TABLE toolchain_jobs
    ADD COLUMN claimed_by text NOT NULL DEFAULT '',
    ADD COLUMN lease_expires_at timestamptz;

CREATE TABLE toolchain_job_logs (
    job_id uuid NOT NULL REFERENCES toolchain_jobs(id) ON DELETE CASCADE,
    seq bigint NOT NULL,
    stream text NOT NULL DEFAULT '',
    line text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, seq)
);

ALTER TABLE toolchain_jobs DROP COLUMN log;

-- The per-node claim excludes a node that already has a running job; this
-- partial index keeps that NOT EXISTS cheap.
CREATE INDEX toolchain_jobs_running_server_idx
    ON toolchain_jobs (server_id) WHERE status = 'running';

-- +goose Down
ALTER TABLE toolchain_jobs ADD COLUMN log text NOT NULL DEFAULT '';
DROP TABLE IF EXISTS toolchain_job_logs;
DROP INDEX IF EXISTS toolchain_jobs_running_server_idx;
ALTER TABLE toolchain_jobs DROP COLUMN IF EXISTS lease_expires_at;
ALTER TABLE toolchain_jobs DROP COLUMN IF EXISTS claimed_by;
