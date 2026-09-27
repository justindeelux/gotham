-- name: CreateProvider :one
INSERT INTO providers (
    user_id, name, base_url, client_id, client_secret, redirect_url,
    access_token, refresh_token, token_expires_at, scopes
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetProviderByIDAndUser :one
SELECT * FROM providers WHERE id = $1 AND user_id = $2;

-- name: ListProvidersByUser :many
SELECT * FROM providers
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: UpdateProviderToken :one
UPDATE providers
SET access_token = $2,
    refresh_token = $3,
    token_expires_at = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpsertRepoCache :one
INSERT INTO repos_cache (
    provider_id, external_id, name, full_name, private,
    default_branch, clone_url, ssh_url, html_url, cached_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (provider_id, external_id) DO UPDATE
SET name = EXCLUDED.name,
    full_name = EXCLUDED.full_name,
    private = EXCLUDED.private,
    default_branch = EXCLUDED.default_branch,
    clone_url = EXCLUDED.clone_url,
    ssh_url = EXCLUDED.ssh_url,
    html_url = EXCLUDED.html_url,
    cached_at = now()
RETURNING *;

-- name: ListRepoCacheByProvider :many
SELECT * FROM repos_cache
WHERE provider_id = $1
ORDER BY full_name ASC;

-- name: DeleteRepoCacheByProvider :exec
DELETE FROM repos_cache WHERE provider_id = $1;
