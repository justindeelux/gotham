-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (
    id, name, kind, config, enabled, resource_type, resource_id, events
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetNotificationChannel :one
SELECT * FROM notification_channels WHERE id = $1;

-- name: ListNotificationChannels :many
SELECT * FROM notification_channels
ORDER BY created_at ASC, id ASC;

-- name: ListNotificationChannelsForEvent :many
-- The dispatcher read: enabled channels that cover the event's resource —
-- instance-wide rows (resource_type IS NULL) plus the overrides scoped to that
-- exact resource. Event-list filtering stays in Go.
SELECT * FROM notification_channels
WHERE enabled
  AND (resource_type IS NULL OR (resource_type = $1 AND resource_id = $2))
ORDER BY created_at ASC, id ASC;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET name = $2,
    kind = $3,
    config = $4,
    enabled = $5,
    resource_type = $6,
    resource_id = $7,
    events = $8,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels WHERE id = $1;

-- name: CreateNotificationDelivery :one
INSERT INTO notification_deliveries (id, channel_id, event_type, status, error_summary, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: LockNotificationChannelHistory :exec
-- Serializes the delivery writers of one channel for the current transaction,
-- so the insert + prune pair cannot race past the retention cap: under READ
-- COMMITTED two concurrent writers would each keep their own newest row and
-- leave the channel at cap+1. The advisory lock releases at commit/rollback.
-- hashtextextended maps the channel UUID to the bigint key; a hash collision
-- only means two channels briefly serialize, which is harmless.
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: ListNotificationDeliveries :many
-- The history panel read: the newest attempts of one channel.
-- created_at DESC, id DESC is the retention order too.
SELECT d.* FROM notification_deliveries d
WHERE d.channel_id = $1
ORDER BY d.created_at DESC, d.id DESC
LIMIT $2;

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
