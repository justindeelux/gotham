-- name: CreateApplicationDeployKey :one
INSERT INTO application_deploy_keys (
    application_id, private_key_id, provider, repo, provider_key_id, fingerprint, public_key
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetApplicationDeployKey :one
SELECT * FROM application_deploy_keys
WHERE application_id = $1;

-- name: DeleteApplicationDeployKey :one
DELETE FROM application_deploy_keys
WHERE application_id = $1
RETURNING *;
