-- name: CreateDatabase :one
INSERT INTO databases (
    id, user_id, server_id, environment_id, name, engine, version, status, public_port, storage_path, team_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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

-- name: ListDatabasesByEnvironment :many
SELECT * FROM databases
WHERE environment_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: ListDatabasesByProject :many
SELECT d.* FROM databases d
JOIN environments e ON e.id = d.environment_id
WHERE e.project_id = $1 AND d.deleted_at IS NULL
ORDER BY d.created_at DESC, d.id DESC;

-- name: CountDatabasesByEnvironment :one
SELECT count(*) FROM databases WHERE environment_id = $1 AND deleted_at IS NULL;

-- name: CountDatabasesByProject :one
SELECT count(*) FROM databases d
JOIN environments e ON e.id = d.environment_id
WHERE e.project_id = $1 AND d.deleted_at IS NULL;

-- name: PurgeTombstonedDatabasesByEnvironment :many
-- Hard-deletes the soft-deleted databases of one environment and returns
-- their storage paths. An environment delete purges these in the same
-- transaction first, so only live databases block it (409). A returned
-- volume may still exist on the node (the retention sweeper only sees rows),
-- so the caller logs it; the data stays recoverable from the node.
DELETE FROM databases
WHERE environment_id = $1 AND deleted_at IS NOT NULL
RETURNING storage_path;

-- name: PurgeTombstonedDatabasesByProject :many
-- Same as above for every environment of one project.
DELETE FROM databases
WHERE environment_id IN (SELECT id FROM environments WHERE project_id = $1)
AND deleted_at IS NOT NULL
RETURNING storage_path;

-- name: PurgeTombstonedDatabasesByServer :many
-- Same as above for one node.
DELETE FROM databases
WHERE server_id = $1 AND deleted_at IS NOT NULL
RETURNING storage_path;

-- name: ListDatabasesByServer :many
SELECT id, name FROM databases
WHERE server_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: DatabaseNameInEnvironment :one
-- The move-collision pre-check: whether the environment holds another live
-- database with the name (exact match, like the unique index).
SELECT EXISTS (
    SELECT 1 FROM databases
    WHERE environment_id = $1 AND name = $2 AND id <> $3 AND deleted_at IS NULL
);

-- name: UpdateDatabaseName :one
UPDATE databases
SET name = $2,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateDatabaseTarget :one
-- Rename, move environment and change node in one write, with the placement
-- columns conditional (COALESCE with narg): the service zeroes placement
-- fields the request leaves alone, so a stale snapshot can never write back
-- an old environment or server. The row stays live throughout: the write is
-- fenced on deleted_at, so a concurrent delete wins and the move silently
-- no-ops to ErrNotFound instead.
UPDATE databases
SET name = $2,
    environment_id = COALESCE(sqlc.narg(environment_id), environment_id),
    server_id = COALESCE(sqlc.narg(server_id), server_id),
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
