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
-- Fenced on the mapping identity the caller read: a delete that lost a race
-- (the row was replaced by a fresh key after the read) matches nothing and
-- cannot remove the replacement.
DELETE FROM application_deploy_keys
WHERE id = $1 AND application_id = $2
RETURNING *;
