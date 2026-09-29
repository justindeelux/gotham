-- name: UpsertPreviewDeploy :one
INSERT INTO preview_deploys (
    application_id, team_id, provider, repo, pr_number, branch, head_sha,
    preview_application_id, host, state
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (application_id, pr_number) DO UPDATE SET
    team_id = EXCLUDED.team_id,
    provider = EXCLUDED.provider,
    repo = EXCLUDED.repo,
    branch = EXCLUDED.branch,
    head_sha = EXCLUDED.head_sha,
    preview_application_id = EXCLUDED.preview_application_id,
    host = EXCLUDED.host,
    state = EXCLUDED.state,
    deleted_at = NULL,
    updated_at = now()
RETURNING *;

-- name: GetPreviewDeploy :one
SELECT * FROM preview_deploys
WHERE application_id = $1 AND pr_number = $2;

-- name: ListPreviewDeploysByApplication :many
SELECT * FROM preview_deploys
WHERE application_id = $1
ORDER BY created_at DESC;

-- name: MarkPreviewDeployClosing :one
-- Persists a close intent before the sibling is torn down, so a failed (or
-- lost) teardown is re-attempted by the sweep.
UPDATE preview_deploys
SET state = 'closing', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkPreviewDeployClosed :one
-- The close completion, wrapped with ClearPreviewDeliveries in one
-- transaction by Store.MarkPreviewClosed. Used on every close path, including
-- the already-deleted retry, so a stale reservation can never poison a
-- reopen.
UPDATE preview_deploys
SET state = 'deleted', deleted_at = now(), updated_at = now()
WHERE application_id = $1 AND pr_number = $2
RETURNING *;

-- name: ListClosingPreviewDeploys :many
-- Bindings whose close intent is older than the sweep grace period: the sweep
-- re-attempts their teardown.
SELECT * FROM preview_deploys
WHERE state = 'closing' AND updated_at < $1
ORDER BY updated_at ASC;

-- name: ListOrphanedPreviewDeploys :many
-- Live bindings whose sibling application no longer exists (deleted out of
-- band, or never linked because preview_application_id is NULL): the sweep
-- closes these without touching any live preview.
SELECT p.* FROM preview_deploys p
LEFT JOIN applications a ON a.id = p.preview_application_id
WHERE p.state <> 'deleted'
  AND (p.preview_application_id IS NULL OR a.id IS NULL);

-- name: ListLivePreviewDeploysForSibling :many
-- The bindings that point at a sibling application being deleted (the
-- user-facing delete path): the binding is the audit trail, not the owner.
-- Store.MarkPreviewDeploysDeletedForSibling closes each of them and clears
-- their ledgers under the base-application lock.
SELECT id, application_id, pr_number FROM preview_deploys
WHERE preview_application_id = $1 AND state <> 'deleted'
ORDER BY application_id, pr_number;

-- name: ListOrphanedPreviewApplications :many
-- Preview siblings without a LIVE binding, older than the sweep grace period:
-- created before the binding was written (crash), left behind by a base delete
-- whose cleanup could not finish, or kept after their binding was marked
-- deleted (a failed direct sibling delete). Ordinary applications are never
-- matched.
SELECT a.id FROM applications a
WHERE a.is_preview
  AND a.created_at < $1
  AND NOT EXISTS (
      SELECT 1 FROM preview_deploys p
      WHERE p.preview_application_id = a.id AND p.state <> 'deleted'
  )
ORDER BY a.created_at ASC;

-- name: CountLivePreviews :one
-- The quota read: distinct pull requests of one base application that are
-- live (a non-deleted binding) or in flight (an unexpired start lease).
-- Leases close the window between "approved" and "binding written" that a
-- plain binding count leaves open under concurrency. $2 excludes the pull
-- request the caller is acting on: its own in-flight lease already holds a
-- slot and must not deny a new head of the same PR.
SELECT count(*) FROM (
    SELECT p.pr_number AS pr_number FROM preview_deploys p
    WHERE p.application_id = $1 AND p.pr_number <> $2 AND p.state <> 'deleted'
    UNION
    SELECT d.pr_number AS pr_number FROM preview_deliveries d
    WHERE d.application_id = $1 AND d.pr_number <> $2 AND d.kind = 'start' AND d.expires_at > clock_timestamp()
) AS live;

-- name: GetPreviewDelivery :one
-- Loads one UNEXPIRED ledger row by id: the promotion fence verifies the
-- lease it was issued still exists and has not lapsed. An expired lease is
-- reported as missing, so a stale worker cannot promote a binding.
SELECT * FROM preview_deliveries WHERE id = $1 AND expires_at > clock_timestamp();

-- name: ReservePreviewDelivery :one
-- Insert a delivery reservation (in-flight lease for a start, teardown marker
-- for a close). A replay of the same signed revision while an earlier attempt
-- is still in flight (or a concurrent close of the same PR) conflicts and
-- returns no row; the caller maps pgx.ErrNoRows to a duplicate delivery.
INSERT INTO preview_deliveries (application_id, pr_number, kind, head_sha, delivery_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ReleasePreviewDelivery :exec
-- Removes a reservation that did not lead to a queued deployment, so the
-- host's retry of the delivery can reserve again.
DELETE FROM preview_deliveries WHERE id = $1;

-- name: ClearPreviewDeliveries :exec
-- Clears a pull request's reservations at a lifecycle transition, so the
-- opposite transition (reopen) can reserve again.
DELETE FROM preview_deliveries
WHERE application_id = $1 AND pr_number = $2;

-- name: PurgeExpiredPreviewDeliveriesFor :exec
-- Removes the pull request's expired reservations. A lease that outlived its
-- delivery (a crash) must never suppress a later delivery of the same
-- revision.
DELETE FROM preview_deliveries
WHERE application_id = $1 AND pr_number = $2 AND expires_at <= clock_timestamp();

-- name: PurgeExpiredPreviewDeliveries :execrows
-- Removes every expired reservation — the sweep's housekeeping pass.
DELETE FROM preview_deliveries WHERE expires_at <= clock_timestamp();
