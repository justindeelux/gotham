-- name: ListOAuthProviders :many
SELECT * FROM oauth_providers ORDER BY name;

-- name: UpsertOAuthProvider :one
INSERT INTO oauth_providers (name, enabled, client_id, client_secret, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (name) DO UPDATE
SET enabled = EXCLUDED.enabled,
    client_id = EXCLUDED.client_id,
    client_secret = EXCLUDED.client_secret,
    updated_at = now()
RETURNING *;
