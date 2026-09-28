-- name: CreateService :one
INSERT INTO services (
    id, user_id, server_id, name, status, compose_yaml, env
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetService :one
SELECT * FROM services
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListServicesByUser :many
SELECT * FROM services
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: UpdateServiceConfig :one
UPDATE services
SET name = $2,
    compose_yaml = $3,
    env = $4,
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
