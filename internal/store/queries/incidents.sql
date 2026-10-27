-- name: CreateIncident :one
INSERT INTO incidents (
    id, identity_key, detector_key, scope_kind, scope_id, node_id, project_id,
    resource_type, resource_id, resource_name, severity, lifecycle, coverage,
    first_observed_at, last_observed_at, last_valid_at, confirmed_at,
    recovery_started_at, resolved_at, close_reason, acknowledged_at,
    acknowledged_by, snoozed_until, snooze_notified_until, delivery_state,
    delivery_hold, delivery_event, delivery_attempts, delivery_error,
    last_notified_severity, evidence, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
    $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31,
    $32, $33
)
ON CONFLICT (identity_key) WHERE lifecycle <> 'resolved' DO NOTHING
RETURNING *;

-- name: GetActiveIncidentByIdentity :one
SELECT * FROM incidents
WHERE identity_key = $1 AND lifecycle <> 'resolved'
FOR UPDATE;

-- name: GetIncident :one
SELECT * FROM incidents WHERE id = $1;

-- name: GetIncidentForUpdate :one
SELECT * FROM incidents WHERE id = $1 FOR UPDATE;

-- name: ListIncidents :many
-- is_owner is resolved from the database role before this query; members only
-- see incidents whose project grant still exists. Then active rows are ordered
-- and limited, so a member cannot count or page through another project.
SELECT i.* FROM incidents i
WHERE (
        sqlc.arg(is_owner)::boolean
        OR (
            i.project_id IS NOT NULL
            AND EXISTS (
                SELECT 1 FROM project_members m
                WHERE m.user_id = sqlc.arg(user_id) AND m.project_id = i.project_id
            )
        )
    )
    AND (sqlc.arg(include_resolved)::boolean OR i.lifecycle <> 'resolved')
    AND (sqlc.narg(lifecycle_filter)::text IS NULL OR i.lifecycle = sqlc.narg(lifecycle_filter))
    AND (sqlc.narg(severity_filter)::text IS NULL OR i.severity = sqlc.narg(severity_filter))
    AND (
        sqlc.narg(cursor_at)::timestamptz IS NULL
        OR (i.updated_at, i.id) < (sqlc.narg(cursor_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
    )
ORDER BY i.updated_at DESC, i.id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListActiveNodeIncidents :many
SELECT * FROM incidents
WHERE node_id IS NOT NULL AND lifecycle <> 'resolved';

-- name: UpdateIncident :one
UPDATE incidents
SET severity = $2,
    lifecycle = $3,
    coverage = $4,
    first_observed_at = $5,
    last_observed_at = $6,
    last_valid_at = $7,
    confirmed_at = $8,
    recovery_started_at = $9,
    resolved_at = $10,
    close_reason = $11,
    snooze_notified_until = $12,
    delivery_state = $13,
    delivery_hold = $14,
    delivery_event = $15,
    delivery_attempts = $16,
    delivery_error = $17,
    last_notified_severity = $18,
    evidence = $19,
    updated_at = $20
WHERE id = $1
RETURNING *;

-- name: AcknowledgeIncident :one
UPDATE incidents
SET acknowledged_at = $2, acknowledged_by = $3, updated_at = $2
WHERE id = $1 AND lifecycle <> 'resolved'
RETURNING *;

-- name: SnoozeIncident :one
UPDATE incidents
SET snoozed_until = $2, snooze_notified_until = NULL, updated_at = $3
WHERE id = $1 AND lifecycle <> 'resolved'
RETURNING *;

-- name: ClearExpiredSnooze :one
UPDATE incidents
SET snoozed_until = NULL, snooze_notified_until = $2, updated_at = $3
WHERE id = $1 AND snoozed_until IS NOT NULL AND snoozed_until <= $3
RETURNING *;

-- name: ListExpiredSnoozes :many
SELECT * FROM incidents
WHERE lifecycle = 'firing'
  AND snoozed_until IS NOT NULL
  AND snoozed_until <= $1
ORDER BY snoozed_until ASC
LIMIT $2;

-- name: CreateIncidentEvent :exec
INSERT INTO incident_events (
    id, incident_id, kind, from_lifecycle, to_lifecycle, from_severity,
    to_severity, reason, actor_id, detail, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ListIncidentEvents :many
SELECT * FROM incident_events
WHERE incident_id = $1
  AND (
      sqlc.narg(cursor_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: PruneIncidentEvents :execrows
DELETE FROM incident_events AS e
WHERE e.incident_id = $1
  AND e.id NOT IN (
      SELECT keep.id FROM incident_events AS keep
      WHERE keep.incident_id = $1
      ORDER BY keep.created_at DESC, keep.id DESC
      LIMIT $2
  );

-- name: CreateIncidentDeliveryIntent :exec
INSERT INTO incident_delivery_intents (
    id, incident_id, transition, severity, state, attempts, last_error,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, 'pending', 0, '', $5, $5);

-- name: ClaimPendingIncidentDeliveries :many
-- Handoff attempts that still need the notifications layer. Claiming flips the
-- row to 'sending' in the same statement, so overlapping Report/Sweep calls
-- hand each intent off exactly once. A failed handoff stays retryable; provider
-- retry is a later package.
WITH claimed AS (
    SELECT d.id FROM incident_delivery_intents AS d
    WHERE d.state IN ('pending', 'failed') AND d.attempts < $2
    ORDER BY d.created_at ASC, d.id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE incident_delivery_intents AS i
SET state = 'sending', updated_at = now()
FROM claimed
WHERE i.id = claimed.id
RETURNING i.*;

-- name: ReclaimStaleIncidentDeliveries :execrows
-- A crash between claim and result leaves 'sending' behind; after a short
-- timeout it becomes retryable again instead of being stranded forever.
UPDATE incident_delivery_intents
SET state = 'failed', last_error = 'handoff interrupted', updated_at = now()
WHERE state = 'sending' AND updated_at < now() - interval '2 minutes';

-- name: ListIncidentDeliveries :many
SELECT * FROM incident_delivery_intents
WHERE incident_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: MarkIncidentDelivery :exec
UPDATE incident_delivery_intents
SET state = $2, attempts = attempts + 1, last_error = $3, updated_at = $4
WHERE id = $1;

-- name: SetIncidentDelivery :exec
UPDATE incidents
SET delivery_state = $2,
    delivery_hold = $3,
    delivery_event = $4,
    delivery_attempts = delivery_attempts + $5,
    delivery_error = $6,
    last_notified_severity = $7,
    updated_at = $8
WHERE id = $1;

-- name: PruneResolvedIncidents :execrows
DELETE FROM incidents
WHERE lifecycle = 'resolved' AND resolved_at < $1;
