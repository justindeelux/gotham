-- name: CreateGitHubApp :one
INSERT INTO github_apps (
    user_id, app_id, slug, name, base_url, api_base_url, client_id,
    webhook_secret_cipher, private_key_cipher
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetGitHubAppByIDAndUser :one
SELECT * FROM github_apps WHERE id = $1 AND user_id = $2;

-- name: GetGitHubAppByID :one
SELECT * FROM github_apps WHERE id = $1;

-- name: ListGitHubAppsByUser :many
SELECT * FROM github_apps
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: DeleteGitHubApp :execrows
DELETE FROM github_apps WHERE id = $1 AND user_id = $2;

-- name: UpsertGitHubInstallation :one
INSERT INTO github_installations (github_app_id, installation_id, account)
VALUES ($1, $2, $3)
ON CONFLICT (github_app_id, installation_id) DO UPDATE
SET account = EXCLUDED.account,
    updated_at = now()
RETURNING *;

-- name: ListGitHubInstallations :many
SELECT * FROM github_installations
WHERE github_app_id = $1
ORDER BY installation_id ASC;

-- name: DeleteGitHubInstallation :execrows
DELETE FROM github_installations WHERE id = $1;

-- name: UpsertGitHubRepoCache :one
INSERT INTO github_repo_cache (
    github_app_id, installation_id, external_id, name, full_name, private,
    default_branch, clone_url, ssh_url, html_url, cached_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (github_app_id, installation_id, external_id) DO UPDATE
SET name = EXCLUDED.name,
    full_name = EXCLUDED.full_name,
    private = EXCLUDED.private,
    default_branch = EXCLUDED.default_branch,
    clone_url = EXCLUDED.clone_url,
    ssh_url = EXCLUDED.ssh_url,
    html_url = EXCLUDED.html_url,
    cached_at = now()
RETURNING *;

-- name: ListGitHubRepoCache :many
SELECT * FROM github_repo_cache
WHERE github_app_id = $1 AND installation_id = $2
ORDER BY full_name ASC;

-- name: DeleteGitHubRepoCache :exec
DELETE FROM github_repo_cache WHERE github_app_id = $1 AND installation_id = $2;

-- name: CountGitHubAppApplications :one
SELECT count(*)::bigint FROM applications
WHERE user_id = $1 AND source_type = 'github_app' AND provider = 'github';

-- name: CountGitHubAppApplicationsForApp :one
SELECT count(*)::bigint FROM applications
WHERE user_id = $1 AND source_type = 'github_app' AND provider = 'github'
AND lower(repo) IN (
    SELECT lower(full_name) FROM github_repo_cache WHERE github_app_id = $2
);

-- name: ListGitHubAppPushTargets :many
SELECT id, branch FROM applications
WHERE user_id = $1 AND source_type = 'github_app' AND provider = 'github'
AND lower(repo) = $2;

-- name: ListGitHubAppsByInstallationID :many
SELECT g.* FROM github_apps g
JOIN github_installations i ON i.github_app_id = g.id
WHERE i.installation_id = $1;
