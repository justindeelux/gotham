-- Queries for the persisted tool install/uninstall queue (JUS-176). The
-- control plane only ever moves a job forward: queued -> running -> terminal.
-- Every writer keys on the row id, and ClaimNextQueuedToolchainJob is the one
-- ordered, concurrency-safe read that hands a worker its next job.

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
SELECT * FROM toolchain_jobs
WHERE server_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ClaimNextQueuedToolchainJob :one
-- Atomically claims the oldest queued job across all nodes, marking it running.
-- FOR UPDATE SKIP LOCKED lets several control-plane instances work the same
-- queue without ever claiming one job twice; no rows means the queue is empty.
UPDATE toolchain_jobs
SET status = 'running', started_at = now()
WHERE id = (
    SELECT id FROM toolchain_jobs
    WHERE status = 'queued'
    ORDER BY created_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: AppendToolchainJobLog :one
-- Appends one log chunk (the caller supplies the trailing newline) so a viewer
-- that fetches the job sees progress while it is still running.
UPDATE toolchain_jobs
SET log = log || $2
WHERE id = $1
RETURNING *;

-- name: FinishToolchainJob :one
-- Closes a job with its terminal status and an optional final log chunk.
UPDATE toolchain_jobs
SET status = $2, log = log || $3, finished_at = now()
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

-- name: FailInterruptedToolchainJobs :many
-- Boot recovery: a job left running belongs to a control plane that died
-- mid-install, so it is marked failed with an explicit reason rather than
-- silently retried (retrying an unknown half-finished install is unsafe).
UPDATE toolchain_jobs
SET status = 'failed', finished_at = now(), log = log || $1
WHERE status = 'running'
RETURNING id;
