-- +goose Up
-- Enforce one running job per node in the schema (PR #325 review H1). The
-- claim's `NOT EXISTS (running on this server)` runs on a READ COMMITTED
-- snapshot, while FOR UPDATE SKIP LOCKED only locks the candidate row, so two
-- concurrent claims could each pick a different job of one node and both mark
-- it running (verified: 39/40 trials). A unique partial index makes the loser
-- fail with 23505, which the store maps to "nothing claimable"; the winner
-- keeps the node serialized.
--
-- This replaces the plain running index from 00054 (the unique one also
-- serves that lookup).
DROP INDEX IF EXISTS toolchain_jobs_running_server_idx;

-- A database that ran a predecessor of this PR may already hold duplicate
-- running rows per node (exactly the rows the racy claim produced, or ones a
-- crash left behind). CREATE UNIQUE INDEX would fail on them and abort the
-- migration, so first fail all but the oldest running job per node (oldest by
-- started_at, then id). This is a no-op on clean data and keeps each node's
-- live job.
UPDATE toolchain_jobs
SET status = 'failed', finished_at = now()
WHERE status = 'running'
  AND id NOT IN (
      SELECT DISTINCT ON (server_id) id
      FROM toolchain_jobs
      WHERE status = 'running'
      ORDER BY server_id, started_at, id
  );

CREATE UNIQUE INDEX toolchain_jobs_running_server_unique
    ON toolchain_jobs (server_id) WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS toolchain_jobs_running_server_unique;
CREATE INDEX IF NOT EXISTS toolchain_jobs_running_server_idx
    ON toolchain_jobs (server_id) WHERE status = 'running';
