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

-- name: CreateNotificationDelivery :one
INSERT INTO notification_deliveries (id, channel_id, event_type, status, error_summary, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListNotificationDeliveries :many
-- The history panel read: the newest attempts of one channel, joined to its
-- channel so a foreign team's channel can never be read (the join matches no
-- row). created_at DESC, id DESC is the retention order too.
SELECT d.* FROM notification_deliveries d
JOIN notification_channels c ON c.id = d.channel_id
WHERE d.channel_id = $1 AND c.team_id = $2
ORDER BY d.created_at DESC, d.id DESC
LIMIT $3;

-- name: PruneNotificationDeliveries :execrows
-- Retention: keep only the newest $2 attempts of one channel, dropping the
-- rest. Run in the same transaction as the insert so history stays bounded.
DELETE FROM notification_deliveries AS d
WHERE d.channel_id = $1
  AND d.id NOT IN (
      SELECT keep.id FROM notification_deliveries AS keep
      WHERE keep.channel_id = $1
      ORDER BY keep.created_at DESC, keep.id DESC
      LIMIT $2
  );
