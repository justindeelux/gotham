-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, hash, scopes)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, name, hash, scopes, last_used_at, revoked_at, created_at;

-- name: ListAPITokensByUser :many
SELECT id, user_id, name, hash, scopes, last_used_at, revoked_at, created_at
FROM api_tokens
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: GetAPITokenByHash :one
SELECT id, user_id, name, hash, scopes, last_used_at, revoked_at, created_at
FROM api_tokens
WHERE hash = $1;

-- name: GetAPITokenByIDAndUser :one
SELECT id, user_id, name, hash, scopes, last_used_at, revoked_at, created_at
FROM api_tokens
WHERE id = $1
  AND user_id = $2;

-- name: RevokeAPIToken :execrows
UPDATE api_tokens
SET revoked_at = now()
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL;

-- name: TouchAPITokenLastUsed :exec
UPDATE api_tokens
SET last_used_at = $2
WHERE id = $1;
