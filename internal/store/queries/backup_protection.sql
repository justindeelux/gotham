-- name: ListBackupProtectionSchedules :many
-- One row per schedule with the newest qualifying success and the newest
-- running run for the same database/target. The observer only reads this
-- model; it never executes, cancels or mutates a backup job.
SELECT
    s.id,
    s.database_id,
    s.cron,
    s.target_id,
    s.enabled,
    s.last_run_at,
    s.next_run_at,
    s.last_slot_at,
    s.target_changed_at,
    s.max_run_seconds,
    s.created_at,
    s.updated_at,
    d.name AS database_name,
    e.project_id,
    COALESCE(t.name, '') AS target_name,
    success.id AS success_id,
    success.created_at AS success_created_at,
    success.finished_at AS success_finished_at,
    run.id AS run_id,
    run.created_at AS run_created_at,
    run.schedule_id AS run_schedule_id
FROM backup_schedules s
JOIN databases d ON d.id = s.database_id
JOIN environments e ON e.id = d.environment_id
LEFT JOIN backup_targets t ON t.id = s.target_id
LEFT JOIN LATERAL (
    SELECT b.id, b.created_at, b.finished_at
    FROM backups b
    WHERE b.database_id = s.database_id
      AND b.status = 'completed'
      AND b.target_id IS NOT DISTINCT FROM s.target_id
    ORDER BY b.created_at DESC, b.id DESC
    LIMIT 1
) success ON true
LEFT JOIN LATERAL (
    SELECT b.id, b.created_at, b.schedule_id
    FROM backups b
    WHERE b.database_id = s.database_id
      AND b.status = 'running'
      AND b.target_id IS NOT DISTINCT FROM s.target_id
    ORDER BY b.created_at DESC, b.id DESC
    LIMIT 1
) run ON true
WHERE d.deleted_at IS NULL
ORDER BY s.database_id, s.id;

-- name: ListActiveBackupProtectionIncidents :many
SELECT id, identity_key, resource_id FROM incidents
WHERE lifecycle <> 'resolved' AND detector = 'backup_protection';
