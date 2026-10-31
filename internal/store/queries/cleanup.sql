-- name: CountActiveDeploymentsByServer :one
-- A node is busy while any of its applications has a deployment in a
-- non-terminal state (queued through starting; running and failed are terminal).
SELECT count(*)
FROM deployments d
JOIN applications a ON a.id = d.application_id
WHERE a.server_id = $1
  AND d.state NOT IN ('running', 'failed');

-- name: InsertCleanupRun :exec
INSERT INTO cleanup_runs (id, server_id, actor_user_id, trigger, started_at, finished_at, reclaimed_bytes, schedule_id, status, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: InsertCleanupRunItem :exec
INSERT INTO cleanup_run_items (run_id, item_id, category, name, status, message, reclaimed_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListCleanupSchedules :many
SELECT * FROM cleanup_schedules ORDER BY name, id;

-- name: GetCleanupSchedule :one
SELECT * FROM cleanup_schedules WHERE id = $1;

-- name: CreateCleanupSchedule :one
INSERT INTO cleanup_schedules (
    id, name, server_ids, categories, cron, min_age_hours, enabled, next_run_at, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateCleanupSchedule :one
UPDATE cleanup_schedules
SET name = $2,
    server_ids = $3,
    categories = $4,
    cron = $5,
    min_age_hours = $6,
    enabled = $7,
    next_run_at = $8,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteCleanupSchedule :exec
DELETE FROM cleanup_schedules WHERE id = $1;

-- name: ListDueCleanupSchedules :many
SELECT * FROM cleanup_schedules
WHERE enabled AND next_run_at <= $1
ORDER BY next_run_at, id;

-- name: MarkCleanupScheduleRun :one
UPDATE cleanup_schedules
SET last_run_at = $2, next_run_at = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListCleanupRuns :many
SELECT r.id, r.server_id, r.actor_user_id, r.trigger, r.started_at, r.finished_at,
       r.reclaimed_bytes, r.schedule_id, r.status, r.reason,
       s.name AS schedule_name, u.email AS actor_email
FROM cleanup_runs r
LEFT JOIN cleanup_schedules s ON s.id = r.schedule_id
LEFT JOIN users u ON u.id = r.actor_user_id
WHERE (sqlc.narg('server_id')::uuid IS NULL OR r.server_id = sqlc.narg('server_id')::uuid)
  AND (sqlc.narg('before')::timestamptz IS NULL OR r.started_at < sqlc.narg('before')::timestamptz)
ORDER BY r.started_at DESC, r.id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetCleanupRun :one
SELECT r.id, r.server_id, r.actor_user_id, r.trigger, r.started_at, r.finished_at,
       r.reclaimed_bytes, r.schedule_id, r.status, r.reason,
       s.name AS schedule_name, u.email AS actor_email
FROM cleanup_runs r
LEFT JOIN cleanup_schedules s ON s.id = r.schedule_id
LEFT JOIN users u ON u.id = r.actor_user_id
WHERE r.id = $1;

-- name: GetLatestCleanupThresholdRun :one
SELECT * FROM cleanup_runs
WHERE server_id = $1 AND trigger = 'threshold'
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: ListCleanupRunItems :many
SELECT * FROM cleanup_run_items WHERE run_id = $1 ORDER BY category, name, item_id;

-- name: ListCleanupRunCategoryTotals :many
SELECT run_id, category, sum(reclaimed_bytes)::bigint AS reclaimed_bytes
FROM cleanup_run_items
WHERE run_id = ANY(sqlc.arg(run_ids)::uuid[])
GROUP BY run_id, category;

-- name: ListCleanupExclusions :many
SELECT * FROM cleanup_exclusions ORDER BY category, match, created_at;

-- name: InsertCleanupExclusion :one
INSERT INTO cleanup_exclusions (id, server_id, category, match, note, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (COALESCE(server_id, '00000000-0000-0000-0000-000000000000'::uuid), category, match)
DO NOTHING
RETURNING *;

-- name: GetCleanupExclusionByKey :one
SELECT * FROM cleanup_exclusions
WHERE COALESCE(server_id, '00000000-0000-0000-0000-000000000000'::uuid)
    = COALESCE(sqlc.narg('server_id')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND category = $1
  AND match = $2;

-- name: DeleteCleanupExclusion :exec
DELETE FROM cleanup_exclusions WHERE id = $1;
