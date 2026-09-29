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

-- name: ListStalePreviewDeploys :many
SELECT * FROM preview_deploys
WHERE state <> 'deleted' AND updated_at < $1
ORDER BY updated_at ASC;
