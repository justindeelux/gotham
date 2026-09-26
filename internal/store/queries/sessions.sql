-- name: CreateSession :one
INSERT INTO sessions (user_id, refresh_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, user_id, refresh_hash, expires_at, revoked_at, created_at;

-- name: GetSessionByRefreshHash :one
SELECT id, user_id, refresh_hash, expires_at, revoked_at, created_at
FROM sessions
WHERE refresh_hash = $1;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE refresh_hash = $1
  AND revoked_at IS NULL;
