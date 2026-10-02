-- name: CreateDatabase :one
INSERT INTO databases (
    id, user_id, server_id, name, engine, version, status, public_port, storage_path, team_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetDatabase :one
SELECT * FROM databases
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListDatabasesByUser :many
SELECT * FROM databases
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: ListDatabasesByTeam :many
SELECT * FROM databases
WHERE team_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: UpdateDatabaseName :one
UPDATE databases
SET name = $2,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateDatabaseContainer :one
UPDATE databases
SET container_id = $2,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateDatabaseStatus :one
UPDATE databases
SET status = $2,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: PublicPortInUse :one
SELECT EXISTS (
    SELECT 1 FROM databases
    WHERE server_id = $1 AND public_port = $2 AND deleted_at IS NULL
);

-- name: DeleteDatabaseSecrets :exec
DELETE FROM database_secrets
WHERE database_id = $1;

-- name: SoftDeleteDatabase :one
UPDATE databases
SET status = 'deleting',
    deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: ListExpiredDatabases :many
-- Soft-deleted databases whose grace window has elapsed. The retention sweep
-- is the only reader that looks past deleted_at; every API read filters it.
SELECT * FROM databases
WHERE deleted_at IS NOT NULL AND deleted_at <= $1
ORDER BY deleted_at ASC, id ASC;

-- name: PurgeDatabase :execrows
-- Hard-deletes a soft-deleted database after its volume is removed; the
-- database_secrets cascade with it. A live row is never purged.
DELETE FROM databases
WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: CreateDatabaseSecret :one
INSERT INTO database_secrets (database_id, key, ciphertext)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListDatabaseSecrets :many
SELECT * FROM database_secrets
WHERE database_id = $1
ORDER BY key ASC;
