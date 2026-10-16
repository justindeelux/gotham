-- Queries for the persisted tool install/uninstall queue (JUS-176). The
-- control plane only ever moves a job forward: queued -> running -> terminal.
-- Every writer keys on the row id, ClaimNextQueuedToolchainJob is the ordered,
-- concurrency-safe read that hands a worker its next job, and the job log lives
-- in the append-only toolchain_job_logs child table.

-- name: CreateToolchainJob :one
-- Enqueues one job. The partial unique index toolchain_jobs_active_unique
-- makes a second active job for the same (server, tool, action) conflict and
-- return no row, so the caller reports the tool as "skipped" instead of
-- duplicating the queue.
INSERT INTO toolchain_jobs (server_id, tool_id, action)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetToolchainJobForServer :one
-- Scopes a single-job read to its node so a job id from another node cannot be
-- probed (the caller maps no rows to not-found).
SELECT * FROM toolchain_jobs WHERE id = $1 AND server_id = $2;

-- name: ListToolchainJobsByServer :many
-- The job rows only: the log is never loaded here, so listing a node's jobs
-- stays cheap no matter how chatty an install was.
SELECT * FROM toolchain_jobs
WHERE server_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ClaimNextQueuedToolchainJob :one
-- Atomically claims the oldest queued job whose node has no running job
-- (per-node sequential, parallel across nodes), marking it running and
-- recording the claiming instance plus a lease the worker renews. FOR UPDATE
-- SKIP LOCKED lets several control-plane instances work the same queue without
-- claiming one job twice; no rows means nothing is currently claimable.
UPDATE toolchain_jobs
SET status = 'running',
    started_at = now(),
    claimed_by = $1,
    lease_expires_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::int)
WHERE id = (
    SELECT j.id FROM toolchain_jobs j
    WHERE j.status = 'queued'
      AND NOT EXISTS (
          SELECT 1 FROM toolchain_jobs r
          WHERE r.server_id = j.server_id AND r.status = 'running'
      )
    ORDER BY j.created_at, j.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: RenewToolchainJobLease :exec
-- Extends a running job's lease. Called periodically by the worker so a live
-- instance's job is never mistaken for a crashed one.
UPDATE toolchain_jobs
SET lease_expires_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::int)
WHERE id = $1 AND status = 'running';

-- name: FailOwnedToolchainJobs :many
-- Boot recovery: fails running jobs this instance claimed before a restart. It
-- runs once, before any worker starts, so it can never touch a job another live
-- instance is working.
UPDATE toolchain_jobs
SET status = 'failed', finished_at = now()
WHERE status = 'running' AND claimed_by = $1
RETURNING id;

-- name: FailStaleToolchainJobs :many
-- Periodic recovery: fails running jobs whose lease lapsed (a crashed
-- instance's job). A live instance renews its lease, so this never fails live
-- work, and two instances never fail each other's. Retrying an unknown
-- half-finished install is unsafe, so these are failed, not requeued.
UPDATE toolchain_jobs
SET status = 'failed', finished_at = now()
WHERE status = 'running'
  AND (lease_expires_at IS NULL OR lease_expires_at < now())
RETURNING id;

-- name: AppendToolchainJobLog :exec
-- Appends one log line as its own row (the caller supplies the trailing
-- newline), so a chatty install is O(n) small inserts, not O(n^2) rewrites.
INSERT INTO toolchain_job_logs (job_id, seq, stream, line)
VALUES ($1, $2, $3, $4)
ON CONFLICT (job_id, seq) DO NOTHING;

-- name: AppendToolchainJobLogTail :exec
-- Appends one line after whatever is already logged, letting the recovery and
-- truncation paths add a marker without knowing the current max seq.
INSERT INTO toolchain_job_logs (job_id, seq, stream, line)
SELECT $1, COALESCE(MAX(seq), 0) + 1, $2, $3
FROM toolchain_job_logs WHERE job_id = $1;

-- name: ListToolchainJobLogs :many
-- A page of a job's log, oldest first, for incremental fetches (seq > after).
SELECT seq, stream, line FROM toolchain_job_logs
WHERE job_id = $1 AND seq > $2
ORDER BY seq
LIMIT $3;

-- name: FinishToolchainJob :one
-- Closes a job with its terminal status.
UPDATE toolchain_jobs
SET status = $2, finished_at = now()
WHERE id = $1
RETURNING *;

-- name: CancelToolchainJob :one
-- Cancels a job only while it is still queued: a running job has already
-- reached the agent, and a terminal job is immutable. No rows means the caller
-- asked to cancel something that is not cancellable.
UPDATE toolchain_jobs
SET status = 'canceled', finished_at = now()
WHERE id = $1 AND status = 'queued'
RETURNING *;
