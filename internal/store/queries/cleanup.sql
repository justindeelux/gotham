-- name: CountActiveDeploymentsByServer :one
-- A node is busy while any of its applications has a deployment in a
-- non-terminal state (queued through starting; running and failed are terminal).
SELECT count(*)
FROM deployments d
JOIN applications a ON a.id = d.application_id
WHERE a.server_id = $1
  AND d.state NOT IN ('running', 'failed');

-- name: InsertCleanupRun :exec
INSERT INTO cleanup_runs (id, server_id, actor_user_id, trigger, started_at, finished_at, reclaimed_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertCleanupRunItem :exec
INSERT INTO cleanup_run_items (run_id, item_id, category, name, status, message, reclaimed_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7);
