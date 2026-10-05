-- name: CreateService :one
INSERT INTO services (
    id, user_id, server_id, environment_id, name, status, compose_yaml, env, team_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetService :one
SELECT * FROM services
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListServicesByUser :many
SELECT * FROM services
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: ListServicesByTeam :many
SELECT * FROM services
WHERE team_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: ListServicesByEnvironment :many
SELECT * FROM services
WHERE environment_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: ListServicesByProject :many
SELECT s.* FROM services s
JOIN environments e ON e.id = s.environment_id
WHERE e.project_id = $1 AND s.deleted_at IS NULL
ORDER BY s.created_at DESC, s.id DESC;

-- name: CountServicesByEnvironment :one
SELECT count(*) FROM services WHERE environment_id = $1 AND deleted_at IS NULL;

-- name: CountServicesByProject :one
SELECT count(*) FROM services s
JOIN environments e ON e.id = s.environment_id
WHERE e.project_id = $1 AND s.deleted_at IS NULL;

-- name: PurgeTombstonedServicesByEnvironment :execrows
-- Hard-deletes the soft-deleted services of one environment. An
-- environment delete purges these in the same transaction first, so only
-- live services block it (409); the compose volumes intentionally survive
-- (they are the project's data, like on a soft delete).
DELETE FROM services
WHERE environment_id = $1 AND deleted_at IS NOT NULL;

-- name: PurgeTombstonedServicesByProject :execrows
-- Same as above for every environment of one project.
DELETE FROM services
WHERE environment_id IN (SELECT id FROM environments WHERE project_id = $1)
AND deleted_at IS NOT NULL;

-- name: PurgeTombstonedServicesByServer :execrows
-- Same as above for one node.
DELETE FROM services
WHERE server_id = $1 AND deleted_at IS NOT NULL;

-- name: ListServicesByServer :many
SELECT id, name FROM services
WHERE server_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: ServiceNameInEnvironment :one
-- The move-collision pre-check: whether the environment holds another live
-- service with the name (exact match, like the unique index).
SELECT EXISTS (
    SELECT 1 FROM services
    WHERE environment_id = $1 AND name = $2 AND id <> $3 AND deleted_at IS NULL
);

-- name: HasActiveServiceDeploy :one
-- A service with a deploy in flight refuses a server change (409), like an
-- application with a non-terminal deployment.
SELECT EXISTS (
    SELECT 1 FROM service_deploys WHERE service_id = $1 AND state = 'deploying'
);

-- name: HasServiceDeploys :one
-- Whether the service was ever deployed. A deployed service cannot change
-- node: its compose project runs there.
SELECT EXISTS (
    SELECT 1 FROM service_deploys WHERE service_id = $1
);

-- name: UpdateServiceConfig :one
-- Only the name, document, environment and variables are written, and the
-- placement columns only when the caller passes them (COALESCE with narg):
-- the service zeroes placement fields the request leaves alone, so a stale
-- snapshot can never write back an old environment or server.
UPDATE services
SET name = $2,
    compose_yaml = $3,
    env = $4,
    environment_id = COALESCE(sqlc.narg(environment_id), environment_id),
    server_id = COALESCE(sqlc.narg(server_id), server_id),
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateServiceStatus :one
UPDATE services
SET status = $2,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteService :one
UPDATE services
SET status = 'deleting',
    deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: ListRoutableServices :many
-- ListRoutableServices returns every live service with its document and
-- environment, in creation order, so the proxy source can render each one's
-- current domain map (a service that no longer renders is reported as
-- unroutable rather than failing the node's sync).
SELECT * FROM services
WHERE deleted_at IS NULL
ORDER BY created_at ASC, id ASC;

-- name: CreateServiceDeploy :one
INSERT INTO service_deploys (id, service_id, state, compose_yaml)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateServiceDeploy :one
UPDATE service_deploys
SET state = $2,
    error = $3,
    finished_at = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListServiceDeploys :many
SELECT * FROM service_deploys
WHERE service_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;
