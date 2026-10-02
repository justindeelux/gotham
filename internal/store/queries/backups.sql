-- name: CreateBackup :one
INSERT INTO backups (
    id, database_id, schedule_id, type, status, size, location,
    target_id, container_id, error, finished_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetBackup :one
SELECT b.*
FROM backups b
    JOIN databases d ON d.id = b.database_id
WHERE b.id = $1 AND d.deleted_at IS NULL;

-- name: ListBackupsByDatabase :many
SELECT b.*
FROM backups b
    JOIN databases d ON d.id = b.database_id
WHERE b.database_id = $1 AND d.deleted_at IS NULL
ORDER BY b.created_at DESC, b.id DESC
LIMIT $2;

-- name: HasBackupsForTarget :one
-- A target whose destination changed would strand every backup that records
-- (or is about to record) a location against the old endpoint/bucket, so the
-- update path asks this first. A running backup counts: it captured the old
-- configuration and will finish by recording the old destination.
SELECT EXISTS (
    SELECT 1 FROM backups
    WHERE target_id = $1 AND status IN ('running', 'completed')
) AS has_backups;

-- name: FinishBackup :one
UPDATE backups
SET status = $2,
    size = $3,
    location = $4,
    error = $5,
    container_id = $6,
    finished_at = $7
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

-- name: GetBackupTarget :one
SELECT * FROM backup_targets
WHERE id = $1;

-- name: ListBackupTargetsByUser :many
SELECT * FROM backup_targets
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

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
INSERT INTO backup_schedules (id, database_id, cron, target_id, enabled, next_run_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBackupSchedule :one
SELECT s.*
FROM backup_schedules s
    JOIN databases d ON d.id = s.database_id
WHERE s.id = $1 AND d.deleted_at IS NULL;

-- name: ListBackupSchedulesByDatabase :many
SELECT s.*
FROM backup_schedules s
    JOIN databases d ON d.id = s.database_id
WHERE s.database_id = $1 AND d.deleted_at IS NULL
ORDER BY s.created_at DESC, s.id DESC;

-- name: UpdateBackupSchedule :one
UPDATE backup_schedules
SET cron = $2,
    target_id = $3,
    enabled = $4,
    next_run_at = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteBackupSchedule :one
DELETE FROM backup_schedules
WHERE id = $1
RETURNING *;

-- name: ListDueBackupSchedules :many
SELECT s.*
FROM backup_schedules s
    JOIN databases d ON d.id = s.database_id
WHERE s.enabled AND s.next_run_at <= $1 AND d.deleted_at IS NULL
ORDER BY s.next_run_at ASC, s.id ASC;

-- name: MarkBackupScheduleRun :one
UPDATE backup_schedules
SET last_run_at = $2,
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
