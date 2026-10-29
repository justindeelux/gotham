-- name: CreateBackup :one
INSERT INTO backups (
    id, database_id, application_id, service_id, schedule_id, type, status,
    size, location, target_id, container_id, error, finished_at,
    pause_during_backup
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetBackup :one
SELECT * FROM backups
WHERE id = $1;

-- name: ListBackupsByResource :many
-- Newest runs of one resource, optionally narrowed by status. Exactly one of
-- the resource ids is set by the caller, who has already been granted access
-- to that resource.
SELECT * FROM backups
WHERE (database_id = sqlc.narg('database_id')::uuid
    OR application_id = sqlc.narg('application_id')::uuid
    OR service_id = sqlc.narg('service_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: ListBackups :many
-- The Backups page: newest runs across every resource the viewer can reach.
-- The grant is applied before LIMIT/OFFSET, so another project's runs can
-- never fill (or starve) a member's page. Ordering is deterministic so an
-- offset page is stable between requests.
SELECT b.*
FROM backups b
    LEFT JOIN databases d ON d.id = b.database_id
    LEFT JOIN applications a ON a.id = b.application_id
    LEFT JOIN services v ON v.id = b.service_id
    LEFT JOIN environments e ON e.id = COALESCE(d.environment_id, a.environment_id, v.environment_id)
WHERE can_access_environment(sqlc.arg('viewer_id')::uuid, e.id)
  AND (d.id IS NULL OR d.deleted_at IS NULL)
  AND (v.id IS NULL OR v.deleted_at IS NULL)
  AND (sqlc.narg('status')::text IS NULL OR b.status = sqlc.narg('status')::text)
ORDER BY b.created_at DESC, b.id DESC
LIMIT $1 OFFSET $2;

-- name: HasBackupsForTarget :one
-- A provider whose destination changed would strand every backup that records
-- (or is about to record) a location against the old endpoint/bucket, so the
-- update path asks this first. A running backup counts: it captured the old
-- configuration and will finish by recording the old destination.
SELECT EXISTS (
    SELECT 1 FROM backups
    WHERE target_id = $1 AND status IN ('running', 'completed')
) AS has_backups;

-- name: FinishBackup :one
-- The checksum is known once the artifact is written, so it lands with the
-- outcome instead of at insert time.
UPDATE backups
SET status = $2,
    size = $3,
    location = $4,
    error = $5,
    container_id = $6,
    finished_at = $7,
    checksum_sha256 = $8,
    checksum_algo = $9
WHERE id = $1
RETURNING *;

-- name: DeleteBackup :one
DELETE FROM backups
WHERE id = $1
RETURNING *;

-- name: CreateBackupTarget :one
INSERT INTO backup_targets (id, user_id, name, kind, endpoint, region, bucket, prefix)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: EnsureBuiltinBackupTarget :one
-- Idempotent: created on first start for every account, so a fresh install can
-- back up with no setup. Never turns a user-created provider into the built-in
-- one (an existing row keeps its flag); callers resolve an existing builtin
-- first and treat a name conflict as "already seeded" rather than duplicating.
INSERT INTO backup_targets (user_id, name, kind, builtin)
VALUES ($1, 'Local disk', 'local', true)
ON CONFLICT (user_id, name) DO UPDATE SET builtin = backup_targets.builtin
RETURNING *;

-- name: GetBuiltinBackupTarget :one
SELECT * FROM backup_targets
WHERE user_id = $1 AND builtin
ORDER BY created_at, id
LIMIT 1;

-- name: GetBackupTarget :one
SELECT * FROM backup_targets
WHERE id = $1;

-- name: GetBackupTargetForShare :one
-- Run start locks the target it references, so a concurrent destination edit
-- (which takes FOR UPDATE) cannot commit between the target read and the run
-- row insert.
SELECT * FROM backup_targets
WHERE id = $1
FOR SHARE;

-- name: GetBackupTargetForUpdate :one
-- The destination-edit transaction locks the target row before checking for
-- referencing runs and updating, closing the check-then-act window.
SELECT * FROM backup_targets
WHERE id = $1
FOR UPDATE;

-- name: ListBackupTargetsByUser :many
SELECT * FROM backup_targets
WHERE user_id = $1
ORDER BY builtin DESC, created_at DESC, id DESC;

-- name: UpdateBackupTarget :one
UPDATE backup_targets
SET name = $2,
    kind = $3,
    endpoint = $4,
    region = $5,
    bucket = $6,
    prefix = $7,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteBackupTarget :one
DELETE FROM backup_targets
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CreateBackupTargetSecret :one
INSERT INTO backup_target_secrets (target_id, key, ciphertext)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListBackupTargetSecrets :many
SELECT * FROM backup_target_secrets
WHERE target_id = $1
ORDER BY key ASC;

-- name: UpsertBackupTargetSecret :one
INSERT INTO backup_target_secrets (target_id, key, ciphertext)
VALUES ($1, $2, $3)
ON CONFLICT (target_id, key)
DO UPDATE SET ciphertext = EXCLUDED.ciphertext
RETURNING *;

-- name: CreateBackupSchedule :one
INSERT INTO backup_schedules (
    id, database_id, application_id, service_id, cron, target_id, enabled,
    next_run_at, max_run_seconds, pause_during_backup, keep_last
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetBackupSchedule :one
SELECT * FROM backup_schedules
WHERE id = $1;

-- name: ListBackupSchedulesByResource :many
SELECT * FROM backup_schedules
WHERE database_id = sqlc.narg('database_id')::uuid
   OR application_id = sqlc.narg('application_id')::uuid
   OR service_id = sqlc.narg('service_id')::uuid
ORDER BY created_at DESC, id DESC;

-- name: ListBackupSchedules :many
-- Every schedule the viewer can reach, filtered by project grant in the query.
SELECT s.*
FROM backup_schedules s
    LEFT JOIN databases d ON d.id = s.database_id
    LEFT JOIN applications a ON a.id = s.application_id
    LEFT JOIN services v ON v.id = s.service_id
    LEFT JOIN environments e ON e.id = COALESCE(d.environment_id, a.environment_id, v.environment_id)
WHERE can_access_environment(sqlc.arg('viewer_id')::uuid, e.id)
  AND (d.id IS NULL OR d.deleted_at IS NULL)
  AND (v.id IS NULL OR v.deleted_at IS NULL)
ORDER BY s.created_at DESC, s.id DESC;

-- name: UpdateBackupSchedule :one
UPDATE backup_schedules
SET cron = $2,
    target_id = $3,
    enabled = $4,
    next_run_at = $5,
    last_slot_at = $6,
    target_changed_at = $7,
    max_run_seconds = $8,
    pause_during_backup = $9,
    keep_last = $10,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteBackupSchedule :one
DELETE FROM backup_schedules
WHERE id = $1
RETURNING *;

-- name: MarkBackupSchedulesTargetChanged :exec
-- A target deletion cascades to NULL on the schedule; record the change so a
-- slot protected by the old target is not re-read as overdue.
UPDATE backup_schedules
SET target_changed_at = now(),
    updated_at = now()
WHERE target_id = $1;

-- name: ListDueBackupSchedules :many
-- Due schedules whose resource still exists; a soft-deleted resource must not
-- keep producing runs while its grace window ticks down.
SELECT s.*
FROM backup_schedules s
WHERE s.enabled AND s.next_run_at <= $1
  AND (s.database_id IS NULL OR EXISTS (
        SELECT 1 FROM databases d WHERE d.id = s.database_id AND d.deleted_at IS NULL))
  AND (s.application_id IS NULL OR EXISTS (
        SELECT 1 FROM applications a WHERE a.id = s.application_id))
  AND (s.service_id IS NULL OR EXISTS (
        SELECT 1 FROM services v WHERE v.id = s.service_id AND v.deleted_at IS NULL))
ORDER BY s.next_run_at ASC, s.id ASC;

-- name: MarkBackupScheduleRun :one
UPDATE backup_schedules
SET last_slot_at = next_run_at,
    last_run_at = $2,
    next_run_at = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListRunningBackups :many
-- Boot-time recovery: rows the control plane left running after a crash or
-- restart can never finish, so they are swept to failed.
SELECT * FROM backups
WHERE status = 'running'
ORDER BY created_at ASC;

-- name: SetBackupWasRunning :exec
-- Records the pre-job container state as observed by the pause, so the
-- boot-time sweep only restarts a resource the job itself stopped.
UPDATE backups
SET was_running = $2
WHERE id = $1;

-- name: ListCompletedScheduledBackups :many
-- Retention only ever considers successful runs of one schedule, newest first.
-- Manual runs and failed runs are never counted towards keep_last.
SELECT * FROM backups
WHERE schedule_id = $1 AND type = 'scheduled' AND status = 'completed'
ORDER BY created_at DESC, id DESC;

-- name: ListFailedBackupsOlderThan :many
-- Failed runs carry no useful artifact; they are pruned after a fixed window.
SELECT * FROM backups
WHERE status = 'failed' AND created_at <= $1
ORDER BY created_at ASC;

-- name: SetBackupCleanupPending :one
UPDATE backups
SET cleanup_pending = true,
    cleanup_error = '',
    cleanup_attempts = 0,
    cleanup_next_attempt_at = $2
WHERE id = $1
RETURNING *;

-- name: SetBackupCleanupError :one
UPDATE backups
SET cleanup_error = $2,
    cleanup_attempts = $3,
    cleanup_next_attempt_at = $4
WHERE id = $1
RETURNING *;

-- name: ListDuePendingCleanups :many
SELECT * FROM backups
WHERE cleanup_pending
  AND (cleanup_next_attempt_at IS NULL OR cleanup_next_attempt_at <= $1)
ORDER BY cleanup_next_attempt_at ASC NULLS FIRST, created_at ASC;

-- name: MarkBackupsProviderRemoved :exec
-- A deleted provider keeps its past runs visible as "provider removed" instead
-- of hiding the fact that an artifact exists somewhere unreachable.
UPDATE backups
SET provider_removed = true
WHERE target_id = $1;

-- name: CountBackupsForTarget :one
SELECT count(*)::bigint AS backup_count FROM backups WHERE target_id = $1;

-- name: ListBackupScheduleIDsForTarget :many
SELECT id FROM backup_schedules WHERE target_id = $1 ORDER BY created_at ASC, id ASC;

-- name: BackupUsageByTarget :many
-- Per-provider usage for the Storage view, from run rows only (no bucket
-- listing): the number of artifacts and their recorded compressed size.
SELECT b.target_id, count(*)::bigint AS backup_count,
    COALESCE(sum(b.size), 0)::bigint AS total_size
FROM backups b
    JOIN backup_targets t ON t.id = b.target_id
WHERE t.user_id = $1
GROUP BY b.target_id;

-- name: ListDueArtifactCleanups :many
-- Artifacts queued by a hard-deleted resource, drained by the cleanup sweeper.
SELECT * FROM backup_artifact_cleanup
WHERE next_attempt_at <= $1
ORDER BY next_attempt_at ASC, id ASC;

-- name: SetArtifactCleanupError :exec
UPDATE backup_artifact_cleanup
SET attempts = $2,
    next_attempt_at = $3,
    last_error = $4
WHERE id = $1;

-- name: DeleteArtifactCleanup :exec
DELETE FROM backup_artifact_cleanup
WHERE id = $1;

-- name: CreateRestore :one
INSERT INTO restores (id, database_id, application_id, service_id, backup_id, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: FinishRestore :one
UPDATE restores
SET status = $2,
    error = $3,
    finished_at = $4
WHERE id = $1
RETURNING *;

-- name: ListRunningRestores :many
-- Boot-time recovery: a restore left running by a crashed control plane can
-- never finish; the sweep fails it and releases its resources.
SELECT * FROM restores
WHERE status = 'running'
ORDER BY created_at ASC;

-- name: ListRestoresByResource :many
-- The durable restore history of one live resource, newest first. Bounded by
-- the caller so the table cannot grow an unbounded response.
SELECT * FROM restores
WHERE database_id = sqlc.narg('database_id')::uuid
   OR application_id = sqlc.narg('application_id')::uuid
   OR service_id = sqlc.narg('service_id')::uuid
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: ListRestores :many
-- Restore history across every resource the viewer can reach; same grant-before-
-- limit rule and deterministic order as ListBackups.
SELECT r.*
FROM restores r
    LEFT JOIN databases d ON d.id = r.database_id
    LEFT JOIN applications a ON a.id = r.application_id
    LEFT JOIN services v ON v.id = r.service_id
    LEFT JOIN environments e ON e.id = COALESCE(d.environment_id, a.environment_id, v.environment_id)
WHERE can_access_environment(sqlc.arg('viewer_id')::uuid, e.id)
  AND (d.id IS NULL OR d.deleted_at IS NULL)
  AND (v.id IS NULL OR v.deleted_at IS NULL)
ORDER BY r.created_at DESC, r.id DESC
LIMIT $1 OFFSET $2;
