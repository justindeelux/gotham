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

-- name: MarkPreviewDeployDeleted :one
UPDATE preview_deploys
SET state = 'deleted', deleted_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountLivePreviewDeploys :one
SELECT count(*) FROM preview_deploys
WHERE application_id = $1 AND state <> 'deleted';

-- name: ListOrphanedPreviewDeploys :many
-- Live bindings whose sibling application no longer exists (deleted out of
-- band, or never linked because preview_application_id is NULL): the sweep
-- marks these deleted without touching any live preview.
SELECT p.* FROM preview_deploys p
LEFT JOIN applications a ON a.id = p.preview_application_id
WHERE p.state <> 'deleted'
  AND (p.preview_application_id IS NULL OR a.id IS NULL);

-- name: MarkPreviewDeploysDeletedForSibling :exec
-- Marks the bindings that point at a sibling application being deleted (the
-- user-facing delete path): the binding is the audit trail, not the owner.
UPDATE preview_deploys
SET state = 'deleted', deleted_at = now(), updated_at = now()
WHERE preview_application_id = $1 AND state <> 'deleted';

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

-- name: ReservePreviewDelivery :one
-- Insert a delivery reservation. A replay of the same signed revision (or a
-- concurrent close of the same PR) conflicts and returns no row; the caller
-- maps pgx.ErrNoRows to a duplicate delivery.
INSERT INTO preview_deliveries (application_id, pr_number, kind, head_sha, delivery_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: ReleasePreviewDelivery :exec
-- Removes a reservation that did not lead to a queued deployment, so the
-- host's retry of the delivery can reserve again.
DELETE FROM preview_deliveries WHERE id = $1;

-- name: ClearPreviewDeliveries :exec
-- Clears a pull request's reservations at a lifecycle transition (close), so a
-- reopen — even at the same head revision — can reserve again.
DELETE FROM preview_deliveries
WHERE application_id = $1 AND pr_number = $2;
