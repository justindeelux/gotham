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

-- name: UpdateServiceConfig :one
UPDATE services
SET name = $2,
    compose_yaml = $3,
    env = $4,
    environment_id = $5,
    server_id = $6,
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
