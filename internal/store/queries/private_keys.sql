-- name: CreatePrivateKey :one
INSERT INTO private_keys (name, encrypted_key, type, description, public_key)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, type, description, public_key, created_at;

-- name: GetPrivateKeyByID :one
SELECT * FROM private_keys WHERE id = $1;

-- name: ListPrivateKeys :many
SELECT id, name, type, description, public_key, created_at
FROM private_keys
ORDER BY created_at DESC, id DESC;

-- name: SetPrivateKeyMetadata :exec
UPDATE private_keys SET type = $2, public_key = $3 WHERE id = $1;

-- name: UpdatePrivateKeyProfile :one
UPDATE private_keys SET name = $2, description = $3
WHERE id = $1
RETURNING id, name, type, description, public_key, created_at;

-- name: DeletePrivateKey :exec
DELETE FROM private_keys WHERE id = $1;

-- name: CountServersBySSHKeyID :one
SELECT count(*) FROM servers WHERE ssh_key_id = $1;

-- name: CountApplicationDeployKeysByPrivateKeyID :one
SELECT count(*) FROM application_deploy_keys WHERE private_key_id = $1;
