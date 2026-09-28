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

-- name: UpdateApplication :one
UPDATE applications
SET name = $2,
    branch = $3,
    build_pack = $4,
    base_domain = $5,
    port = $6,
    host_port = $7,
    server_id = $8,
    base_domain_disabled = $9,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteApplication :exec
DELETE FROM applications WHERE id = $1;

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

-- name: ClearEnvVarsByApp :exec
DELETE FROM env_vars WHERE application_id = $1;

-- name: InsertEnvVar :one
INSERT INTO env_vars (application_id, key, value)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ClearSecretsByApp :exec
DELETE FROM secrets WHERE application_id = $1;

-- name: InsertSecret :one
-- The id is written explicitly: it is the stable `secret:<id>` reference the
-- API hands out for a sealed value, so re-writing a secret keeps its reference.
INSERT INTO secrets (id, application_id, key, ciphertext)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ClearStoragesByApp :exec
DELETE FROM storages WHERE application_id = $1;

-- name: InsertStorage :one
INSERT INTO storages (application_id, name, host_path, container_path)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: FailStaleDeployments :execrows
-- Boot-time recovery: a deployment left in a non-terminal state by a previous
-- control plane process can never resume, and its row would keep blocking the
-- active-deployment partial unique index. Mark those rows failed so the index
-- unblocks. Running deployments are left alone (their container is the state),
-- and the worker pool is empty when this runs at service construction.
UPDATE deployments
SET state = 'failed',
    error = 'control plane restarted before the deployment finished',
    finished_at = now(),
    updated_at = now()
WHERE state NOT IN ('running', 'failed');
