-- name: CreateApplication :one
-- The id is optional: a caller that must know the application id before the
-- insert (storage host paths are confined to <managed root>/<app id>) passes
-- one, and COALESCE keeps the database-generated default for every other
-- caller.
INSERT INTO applications (
    id, user_id, server_id, environment_id, name, provider, repo, clone_url,
    branch, build_pack, is_preview,
    source_type, github_app_id, dockerfile_content, build_args,
    image_ref, registry_username, registry_password_ciphertext,
    compose_content, compose_file, compose_service
)
VALUES (
    COALESCE(sqlc.arg(id)::uuid, gen_random_uuid()),
    sqlc.arg(user_id), sqlc.arg(server_id), sqlc.arg(environment_id), sqlc.arg(name), sqlc.arg(provider),
    sqlc.arg(repo), sqlc.arg(clone_url), sqlc.arg(branch), sqlc.arg(build_pack),
    -- Direct sqlc callers (fixtures, previews) may pass an empty source
    -- type; COALESCE maps it onto the default so the CHECK never sees it.
    -- The deploy repository normalizes the same way in Go.
    sqlc.arg(is_preview), COALESCE(NULLIF(sqlc.arg(source_type)::text, ''), 'git_public'),
    sqlc.arg(github_app_id)::uuid,
    -- Direct sqlc callers (fixtures, previews) predate the GS-7/GS-8 columns;
    -- COALESCE maps their zero values onto the column defaults.
    COALESCE(sqlc.arg(dockerfile_content)::text, ''),
    COALESCE(sqlc.arg(build_args)::jsonb, '{}'),
    sqlc.arg(image_ref), sqlc.arg(registry_username), sqlc.arg(registry_password_ciphertext),
    COALESCE(sqlc.arg(compose_content)::text, ''),
    COALESCE(sqlc.arg(compose_file)::text, ''),
    COALESCE(sqlc.arg(compose_service)::text, '')
)
RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications WHERE id = $1;

-- name: ListApplicationsByUser :many
SELECT * FROM applications
WHERE can_access_environment(sqlc.arg(user_id)::uuid, environment_id) AND is_preview = false
ORDER BY created_at DESC, id DESC;

-- name: ListApplicationsByEnvironment :many
-- One environment's applications, newest first. Previews stay out of the
-- default listing; the resources endpoint passes true for ?previews=1.
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
-- Live applications only: previews are hidden from the default listing, so
-- the display counts match it. Delete guards consult the preview counts
-- below instead.
SELECT count(*) FROM applications WHERE environment_id = $1 AND is_preview = false;

-- name: CountApplicationsByProject :one
SELECT count(*) FROM applications a
JOIN environments e ON e.id = a.environment_id
WHERE e.project_id = $1 AND a.is_preview = false;

-- name: CountPreviewApplicationsByEnvironment :one
-- Live previews of one environment: they stay out of every count and
-- listing, but they still block the environment delete.
SELECT count(*) FROM applications WHERE environment_id = $1 AND is_preview = true;

-- name: CountPreviewApplicationsByProject :one
-- Live previews of every environment of one project (see above).
SELECT count(*) FROM applications a
JOIN environments e ON e.id = a.environment_id
WHERE e.project_id = $1 AND a.is_preview = true;

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
    server_id = $5,
    environment_id = $6,
    github_app_id = $7,
    dockerfile_content = $8,
    build_args = $9,
    image_ref = $10,
    registry_username = $11,
    registry_password_ciphertext = $12,
    compose_content = $13,
    compose_file = $14,
    compose_service = $15,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteApplication :exec
DELETE FROM applications WHERE id = $1;

-- name: CreateDeployment :one
INSERT INTO deployments (
    application_id, kind, state, image_tag, registry_image, digest, rollback_from,
    compose_document, compose_commit,
    commit_sha, commit_message, commit_author, committed_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7,
    COALESCE(sqlc.arg(compose_document)::text, ''),
    COALESCE(sqlc.arg(compose_commit)::text, ''),
    COALESCE(sqlc.arg(commit_sha)::text, ''),
    COALESCE(sqlc.arg(commit_message)::text, ''),
    COALESCE(sqlc.arg(commit_author)::text, ''),
    COALESCE(sqlc.arg(committed_at)::text, '')
)
RETURNING *;

-- name: GetDeployment :one
SELECT * FROM deployments
WHERE id = $1 AND application_id = $2;

-- name: UpdateDeploymentBuildLog :exec
-- Persists the capped build log of a finished deployment (JUS-84). It runs
-- outside the state-machine writes so a transition can never clobber it.
UPDATE deployments
SET build_log = $2,
    updated_at = now()
WHERE id = $1;

-- name: ListDeploymentsByApp :many
SELECT * FROM deployments
WHERE application_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListDeploymentsByAppLimit :many
SELECT * FROM deployments
WHERE application_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ListRecentDeploymentsByUser :many
-- The newest deployments across the caller's applications (dashboard feed),
-- newest first. Previews stay out, matching the applications list. The heavy
-- columns the feed never returns (build_log, compose_document) are left out
-- on purpose.
SELECT d.id, d.application_id, d.kind, d.state, d.image_tag, d.registry_image,
       d.digest, d.error, d.attempt, d.container_id, d.rollback_from,
       d.started_at, d.finished_at, d.created_at, d.updated_at, d.compose_commit,
       d.commit_sha, d.commit_message, d.commit_author, d.committed_at,
       a.name AS application_name, a.repo AS application_repo, a.branch AS application_branch
FROM deployments d
JOIN applications a ON a.id = d.application_id
WHERE can_access_environment(sqlc.arg(user_id)::uuid, a.environment_id) AND a.is_preview = false
ORDER BY d.created_at DESC, d.id DESC
LIMIT sqlc.arg('limit');

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
    compose_document = $11,
    compose_commit = $12,
    commit_sha = $13,
    commit_message = $14,
    commit_author = $15,
    committed_at = $16,
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
-- and the worker pool is empty when this runs at service construction. Rows
-- with no recorded output get the reason as their stored log (JUS-84) so the
-- row is never a silent empty; a partial log from a previous persist is kept.
UPDATE deployments
SET state = 'failed',
    error = 'control plane restarted before the deployment finished',
    build_log = CASE WHEN build_log = '' THEN 'control plane restarted before the deployment finished' ELSE build_log END,
    finished_at = now(),
    updated_at = now()
WHERE state NOT IN ('running', 'failed');
