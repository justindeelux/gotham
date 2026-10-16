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
CREATE UNIQUE INDEX toolchain_jobs_running_server_unique
    ON toolchain_jobs (server_id) WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS toolchain_jobs_running_server_unique;
CREATE INDEX IF NOT EXISTS toolchain_jobs_running_server_idx
    ON toolchain_jobs (server_id) WHERE status = 'running';
