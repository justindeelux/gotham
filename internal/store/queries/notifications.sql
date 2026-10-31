-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (
    id, name, kind, config, enabled, resource_type, resource_id, events, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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
INSERT INTO notification_deliveries (id, channel_id, event_type, status, error_summary, created_at, dedupe_key, payload, incident_id, next_attempt_at, completed_at, expires_at, attempts)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10::timestamptz, now()), $11, COALESCE($12::timestamptz, now() + interval '24 hours'), $13)
ON CONFLICT (channel_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING
RETURNING *;

-- name: CreatePendingDeliveries :many
-- Durable enqueue: the caller persists every matching event/channel delivery
-- before it wakes a worker. A duplicate producer conflicts on the local
-- identity and returns no row.
INSERT INTO notification_deliveries (id, channel_id, event_type, status, error_summary, created_at, dedupe_key, payload, incident_id, next_attempt_at, expires_at)
SELECT unnest(sqlc.arg(ids)::uuid[]), unnest(sqlc.arg(channel_ids)::uuid[]), sqlc.arg(event_type),
       'pending', '', sqlc.arg(created_at), unnest(sqlc.arg(dedupe_keys)::text[]),
       unnest(sqlc.arg(payloads)::text[]), unnest(sqlc.arg(incident_ids)::uuid[]), sqlc.arg(created_at),
       sqlc.arg(expires_at)
ON CONFLICT (channel_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING
RETURNING *;

-- name: ClaimNotificationDeliveries :many
-- Atomically claim a bounded batch for sending. SKIP LOCKED lets several
-- workers share the queue; the NOT EXISTS preserves per-incident/channel
-- transition order so an opening is not sent after its recovery.
WITH candidate AS (
    SELECT d.id
    FROM notification_deliveries d
    WHERE d.status IN ('pending', 'retrying')
      AND d.next_attempt_at <= sqlc.arg(now)
      AND (
          d.incident_id IS NULL
          OR NOT EXISTS (
              SELECT 1 FROM notification_deliveries earlier
              WHERE earlier.incident_id = d.incident_id
                AND earlier.channel_id = d.channel_id
                AND earlier.status IN ('pending', 'retrying', 'sending')
                AND (earlier.created_at, earlier.id) < (d.created_at, d.id)
          )
      )
    ORDER BY d.next_attempt_at ASC, d.id ASC
    LIMIT sqlc.arg(batch)
    FOR UPDATE SKIP LOCKED
)
UPDATE notification_deliveries d
SET status = 'sending',
    attempts = d.attempts + 1,
    lease_until = sqlc.arg(lease_until),
    updated_at = sqlc.arg(now)
FROM candidate
WHERE d.id = candidate.id
RETURNING d.*;

-- name: ReclaimNotificationDeliveries :execrows
-- A restart or crash leaves rows in 'sending' with a lease that is never
-- renewed. Reclaim them so the next claim pass retries (or terminates once the
-- attempt budget is spent).
UPDATE notification_deliveries
SET status = CASE WHEN attempts >= sqlc.arg(max_attempts) THEN 'terminal_failed' ELSE 'retrying' END,
    next_attempt_at = sqlc.arg(now),
    lease_until = NULL,
    error_summary = CASE WHEN attempts >= sqlc.arg(max_attempts) THEN 'delivery lease expired' ELSE error_summary END,
    completed_at = CASE WHEN attempts >= sqlc.arg(max_attempts) THEN sqlc.arg(now) ELSE NULL END,
    updated_at = sqlc.arg(now)
WHERE status = 'sending' AND lease_until < sqlc.arg(now);

-- name: ExpireNotificationDeliveries :execrows
-- Pending work never outlives the 24-hour delivery age. The age basis is
-- expires_at, which an owner retry moves forward; created_at still orders the
-- incident transitions.
UPDATE notification_deliveries
SET status = 'terminal_failed',
    error_summary = 'delivery expired after 24 hours',
    lease_until = NULL,
    completed_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE status IN ('pending', 'retrying')
  AND expires_at < sqlc.arg(now);

-- name: CleanupNotificationDeliveries :execrows
-- Terminal details are retained for 30 days; pending rows are bounded by the
-- expiry above, not by this cleanup.
DELETE FROM notification_deliveries
WHERE status IN ('terminal_failed', 'cancelled')
  AND completed_at < sqlc.arg(cutoff);

-- name: FinishNotificationDelivery :exec
-- Completion clears the rendered event for delivered/cancelled rows. A
-- terminal failure keeps it so owner Retry can replay the same message; the
-- 30-day terminal cleanup deletes the retained row (and its payload).
UPDATE notification_deliveries
SET status = sqlc.arg(status),
    error_summary = sqlc.arg(error_summary),
    next_attempt_at = sqlc.arg(next_attempt_at),
    lease_until = NULL,
    completed_at = sqlc.narg(completed_at),
    payload = CASE WHEN sqlc.arg(status) IN ('delivered', 'cancelled') THEN '' ELSE payload END,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: CancelNotificationDelivery :exec
UPDATE notification_deliveries
SET status = 'cancelled',
    error_summary = sqlc.arg(error_summary),
    lease_until = NULL,
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id);

-- name: GetNotificationDelivery :one
SELECT * FROM notification_deliveries WHERE id = $1;

-- name: RetryNotificationDelivery :execrows
-- Owner retry keeps the same local identity, so no duplicate inbox event or
-- delivery row is created.
UPDATE notification_deliveries
SET status = 'pending',
    attempts = 0,
    next_attempt_at = sqlc.arg(now),
    expires_at = sqlc.arg(expires_at),
    lease_until = NULL,
    completed_at = NULL,
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND status = 'terminal_failed';

-- name: HasDeliveredIncidentOpening :one
SELECT EXISTS (
    SELECT 1 FROM notification_deliveries d
    WHERE d.incident_id = $1
      AND d.channel_id = $2
      AND d.status = 'delivered'
      AND d.event_type IN ('incident_opened', 'incident_severity_changed', 'runtime_failure')
);

-- name: HasLaterIncidentTransition :one
-- An owner retry of an opening must not send a stale "opened" after a later
-- transition for the same incident/channel already completed or was cancelled.
SELECT EXISTS (
    SELECT 1 FROM notification_deliveries d
    WHERE d.incident_id = $1
      AND d.channel_id = $2
      AND d.status IN ('delivered', 'terminal_failed', 'cancelled')
      AND (d.created_at, d.id) > ($3::timestamptz, $4::uuid)
);

-- name: ListTerminalDeliveryFailures :many
SELECT d.*, c.name AS channel_name, c.kind AS channel_kind
FROM notification_deliveries d
JOIN notification_channels c ON c.id = d.channel_id
WHERE d.status = 'terminal_failed'
ORDER BY d.completed_at DESC, d.id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListNotificationChannelHealth :many
SELECT channel_id,
       max(completed_at) FILTER (WHERE status = 'delivered')::timestamptz AS last_delivered_at,
       max(completed_at) FILTER (WHERE status = 'terminal_failed')::timestamptz AS last_failed_at,
       count(*) FILTER (WHERE status IN ('pending', 'retrying', 'sending')) AS pending_count
FROM notification_deliveries
GROUP BY channel_id;

-- name: ListLatestDeliveryFailures :many
SELECT DISTINCT ON (channel_id) channel_id, error_summary, completed_at
FROM notification_deliveries
WHERE status = 'terminal_failed'
ORDER BY channel_id, completed_at DESC, id DESC;

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
-- Retention: keep only the newest $2 delivered attempts of one channel. Only
-- delivered rows are pruned; in-flight work is never deleted, terminal
-- failures/cancellations keep the 30-day cleanup, and a delivered opening for
-- an active incident is kept while recovery suppression still needs it.
DELETE FROM notification_deliveries AS d
WHERE d.channel_id = $1
  AND d.status = 'delivered'
  AND d.id NOT IN (
      SELECT keep.id FROM notification_deliveries AS keep
      WHERE keep.channel_id = $1
      ORDER BY keep.created_at DESC, keep.id DESC
      LIMIT $2
  )
  AND NOT (
      d.incident_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM incidents i
          WHERE i.id = d.incident_id AND i.lifecycle <> 'resolved'
      )
  );
