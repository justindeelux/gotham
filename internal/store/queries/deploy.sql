-- name: CreateApplication :one
INSERT INTO applications (
    user_id, server_id, name, provider, repo, clone_url,
    branch, build_pack, base_domain, port, host_port
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications WHERE id = $1;

-- name: ListApplicationsByUser :many
SELECT * FROM applications
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: CreateDeployment :one
INSERT INTO deployments (
    application_id, kind, state, image_tag, registry_image, digest, rollback_from
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetDeployment :one
SELECT * FROM deployments
WHERE id = $1 AND application_id = $2;

-- name: ListDeploymentsByApp :many
SELECT * FROM deployments
WHERE application_id = $1
ORDER BY created_at DESC, id DESC;

-- name: GetActiveDeploymentByApp :one
SELECT * FROM deployments
WHERE application_id = $1 AND state NOT IN ('running', 'failed')
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateDeployment :one
UPDATE deployments
SET state = $2,
    image_tag = $3,
    registry_image = $4,
    digest = $5,
    error = $6,
    attempt = $7,
    container_id = $8,
    started_at = $9,
    finished_at = $10,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListEnvVarsByApp :many
SELECT * FROM env_vars
WHERE application_id = $1
ORDER BY key ASC;

-- name: ListSecretsByApp :many
SELECT * FROM secrets
WHERE application_id = $1
ORDER BY key ASC;

-- name: ListStoragesByApp :many
SELECT * FROM storages
WHERE application_id = $1
ORDER BY name ASC;
