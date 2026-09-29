-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (
    id, team_id, name, kind, config, enabled, resource_type, resource_id, events
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetNotificationChannel :one
-- The team filter is what makes a foreign channel ID indistinguishable from a
-- missing one (404).
SELECT * FROM notification_channels WHERE id = $1 AND team_id = $2;

-- name: ListNotificationChannels :many
SELECT * FROM notification_channels
WHERE team_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListNotificationChannelsForEvent :many
-- The dispatcher read: enabled channels of the event's team that cover the
-- event's resource — team-wide rows (resource_type IS NULL) plus the overrides
-- scoped to that exact resource. Event-list filtering stays in Go.
SELECT * FROM notification_channels
WHERE team_id = $1
  AND enabled
  AND (resource_type IS NULL OR (resource_type = $2 AND resource_id = $3))
ORDER BY created_at ASC, id ASC;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET name = $3,
    kind = $4,
    config = $5,
    enabled = $6,
    resource_type = $7,
    resource_id = $8,
    events = $9,
    updated_at = now()
WHERE id = $1 AND team_id = $2
RETURNING *;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels WHERE id = $1 AND team_id = $2;
