-- name: CreateProject :one
INSERT INTO projects (id, team_id, name, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateEnvironment :one
INSERT INTO environments (id, project_id, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProject :one
-- The team filter is what makes a foreign project ID indistinguishable from a
-- missing one (404).
SELECT * FROM projects WHERE id = $1 AND team_id = $2;

-- name: ListProjectsByTeam :many
SELECT * FROM projects
WHERE team_id = $1
ORDER BY created_at DESC, id DESC;

-- name: UpdateProject :one
UPDATE projects
SET name = $3,
    description = $4,
    updated_at = now()
WHERE id = $1 AND team_id = $2
RETURNING *;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1 AND team_id = $2;

-- name: ListEnvironmentsByProject :many
SELECT * FROM environments
WHERE project_id = $1
ORDER BY created_at ASC, id ASC;

-- name: GetEnvironment :one
-- GetEnvironment returns one environment of a team, joining the parent
-- project so a foreign environment ID is indistinguishable from a missing one
-- (404). PE-2 uses the same read for the environment resources surface.
SELECT e.*
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE e.id = $1 AND p.team_id = $2;

-- name: UpdateEnvironment :one
UPDATE environments e
SET name = $2,
    updated_at = now()
FROM projects p
WHERE e.id = $1 AND e.project_id = p.id AND p.team_id = $3
RETURNING e.*;

-- name: GetProjectForUpdate :one
-- GetProjectForUpdate locks the project row for the duration of a guarded
-- environment delete, so two concurrent deletes of one project's last two
-- environments serialize and the survivor check cannot race.
SELECT * FROM projects WHERE id = $1 FOR UPDATE;

-- name: DeleteEnvironment :execrows
DELETE FROM environments e
USING projects p
WHERE e.id = $1 AND e.project_id = p.id AND p.team_id = $2;

-- name: CountEnvironmentsByProject :one
SELECT count(*) FROM environments WHERE project_id = $1;

-- name: ListSharedVariables :many
-- ListSharedVariables returns one scope's variables (environment_id NULL for
-- the project level), ordered by key. A NULL parameter matches the project
-- level through IS NOT DISTINCT FROM.
SELECT * FROM shared_variables
WHERE project_id = $1 AND environment_id IS NOT DISTINCT FROM $2
ORDER BY key ASC;

-- name: ListSharedVariablesForEnvironment :many
-- ListSharedVariablesForEnvironment returns the project-level rows plus one
-- environment's rows in a single snapshot, so the deploy merge reads both
-- scopes without a concurrent replace slipping between two reads.
SELECT * FROM shared_variables
WHERE project_id = $1 AND (environment_id IS NULL OR environment_id = $2)
ORDER BY key ASC;

-- name: DeleteSharedVariables :exec
-- DeleteSharedVariables clears one scope's whole set; the replace path
-- re-inserts the new set in the same transaction.
DELETE FROM shared_variables
WHERE project_id = $1 AND environment_id IS NOT DISTINCT FROM $2;

-- name: InsertSharedVariable :one
INSERT INTO shared_variables (id, project_id, environment_id, key, value, ciphertext, secret)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListPreviewBases :many
-- ListPreviewBases maps preview application rows to their base application
-- for the environment resources surface (PE-5 M1): the preview marker rides
-- the applications row itself, but the base id lives in preview_deploys.
-- Team-scoped, so a foreign preview id simply resolves to no row.
SELECT application_id, preview_application_id
FROM preview_deploys
WHERE preview_application_id = ANY(sqlc.slice('preview_app_ids'))
  AND team_id = sqlc.arg('team_id')
  AND deleted_at IS NULL;
