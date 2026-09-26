-- name: CreateServer :one
INSERT INTO servers (name, ip, port, ssh_user, ssh_key_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetServerByID :one
SELECT * FROM servers WHERE id = $1;

-- name: GetServerByNodeID :one
SELECT * FROM servers WHERE node_id = $1;

-- name: ListServers :many
SELECT * FROM servers ORDER BY created_at DESC, id DESC;

-- name: DeleteServer :exec
DELETE FROM servers WHERE id = $1;

-- name: UpdateServerAgentInfo :one
UPDATE servers
SET node_id = $2,
    os = $3,
    docker_version = $4,
    arch = $5,
    total_mem = $6,
    total_disk = $7,
    status = 'ready',
    last_seen = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateServerMetrics :one
UPDATE servers
SET cpu_usage = $2,
    mem_usage = $3,
    disk_usage = $4,
    container_count = $5,
    status = 'ready',
    last_seen = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetServerStatus :one
UPDATE servers
SET status = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;
