-- name: GetInstanceSettings :one
SELECT * FROM instance_settings WHERE id = 1;

-- name: UpdateInstanceGeneral :one
UPDATE instance_settings
SET control_plane_url = $1, instance_name = $2, timezone = $3,
    pending_control_plane_url = $4, updated_at = now()
WHERE id = 1
RETURNING *;

-- name: UpdateInstanceSystem :one
UPDATE instance_settings
SET hostname = $1, ntp_enabled = $2, ntp_servers = $3, updated_at = now()
WHERE id = 1
RETURNING *;

-- name: UpdateInstanceNetwork :one
UPDATE instance_settings
SET dns_servers = $1,
    ipv4_mode = $2, ipv4_address = $3, ipv4_gateway = $4,
    ipv6_enabled = $5, ipv6_mode = $6, ipv6_address = $7, ipv6_gateway = $8,
    pending_network = $9, pending_deadline = $10,
    updated_at = now()
WHERE id = 1
RETURNING *;

-- name: UpdateInstanceCleanup :one
UPDATE instance_settings
SET cleanup_history_retention_days = $1,
    cleanup_backup_file_min_age_days = $2,
    updated_at = now()
WHERE id = 1
RETURNING *;

-- name: UpdateInstanceThreshold :one
UPDATE instance_settings
SET cleanup_threshold_enabled = $1,
    cleanup_threshold_percent = $2,
    cleanup_threshold_categories = $3,
    cleanup_threshold_min_age_hours = $4,
    cleanup_threshold_cooldown_hours = $5,
    updated_at = now()
WHERE id = 1
RETURNING *;

-- name: InsertInstanceAudit :exec
INSERT INTO instance_settings_audit (actor_id, section, changes) VALUES ($1, $2, $3);
