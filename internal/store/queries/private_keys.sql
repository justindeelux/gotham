-- name: CreatePrivateKey :one
INSERT INTO private_keys (name, encrypted_key, team_id)
VALUES ($1, $2, $3)
RETURNING id, name, created_at;

-- name: GetPrivateKeyByID :one
SELECT * FROM private_keys WHERE id = $1;

-- name: ListPrivateKeys :many
SELECT id, name, created_at
FROM private_keys
ORDER BY created_at DESC, id DESC;

-- name: DeletePrivateKey :exec
DELETE FROM private_keys WHERE id = $1;
