-- name: CreateApplication :one
-- The id is optional: a caller that must know the application id before the
-- insert (storage host paths are confined to <managed root>/<app id>) passes
-- one, and COALESCE keeps the database-generated default for every other
-- caller.
INSERT INTO applications (
    id, user_id, server_id, environment_id, name, provider, repo, clone_url,
    branch, build_pack, base_domain, port, host_port, team_id, is_preview
)
VALUES (
    COALESCE(sqlc.arg(id)::uuid, gen_random_uuid()),
    sqlc.arg(user_id), sqlc.arg(server_id), sqlc.arg(environment_id), sqlc.arg(name), sqlc.arg(provider),
    sqlc.arg(repo), sqlc.arg(clone_url), sqlc.arg(branch), sqlc.arg(build_pack),
    sqlc.arg(base_domain), sqlc.arg(port), sqlc.arg(host_port), sqlc.arg(team_id),
    sqlc.arg(is_preview)
)
RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications WHERE id = $1;

-- name: ListApplicationsByUser :many
SELECT * FROM applications
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListApplicationsByTeam :many
SELECT * FROM applications
WHERE team_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListApplicationsByEnvironment :many
-- One environment's applications, newest first. Previews stay out of the
-- default listing; ?previews=1 passes true to include them.
SELECT * FROM applications
WHERE environment_id = $1 AND (is_preview = false OR sqlc.arg(include_previews)::bool = true)
ORDER BY created_at DESC, id DESC;

-- name: ListApplicationsByProject :many
-- Every environment's applications of one project (the ?project_id= filter),
-- newest first. Previews are included only on request, as above.
SELECT a.* FROM applications a
JOIN environments e ON e.id = a.environment_id
WHERE e.project_id = $1 AND (a.is_preview = false OR sqlc.arg(include_previews)::bool = true)
ORDER BY a.created_at DESC, a.id DESC;

-- name: CountApplicationsByEnvironment :one
SELECT count(*) FROM applications WHERE environment_id = $1;

-- name: CountApplicationsByProject :one
SELECT count(*) FROM applications a
JOIN environments e ON e.id = a.environment_id
WHERE e.project_id = $1;

-- name: ListApplicationsByServer :many
-- One node's applications (id and name only): the server-delete 409 names its
-- blocking resources.
SELECT id, name FROM applications
WHERE server_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ApplicationNameInEnvironment :one
-- The move-collision pre-check: whether the environment holds another
-- application with the name (exact match, like the unique index).
SELECT EXISTS (
    SELECT 1 FROM applications
    WHERE environment_id = $1 AND name = $2 AND id <> $3
);

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
    environment_id = $10,
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

-- name: ListDeploymentsByAppLimit :many
SELECT * FROM deployments
WHERE application_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

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

-- name: LockApplication :exec
-- LockApplication takes a row lock on the parent application, serializing
-- mutations of its child collections (env vars, secrets, storages). Without
-- it, two transactions that clear an empty collection and then insert
-- disjoint keys both see nothing to delete and commit their union — a merge
-- where a replace was requested.
SELECT id FROM applications WHERE id = $1 FOR UPDATE;

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
