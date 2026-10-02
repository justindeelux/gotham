-- name: CreateServer :one
INSERT INTO servers (name, ip, port, ssh_user, ssh_key_id, team_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetServerByID :one
SELECT * FROM servers WHERE id = $1;

-- name: GetServerByNodeID :one
SELECT * FROM servers WHERE node_id = $1;

-- name: ListServers :many
SELECT * FROM servers ORDER BY created_at DESC, id DESC;

-- name: ListServersByTeam :many
-- A legacy node (team_id NULL) predates teams and stays visible to every
-- authenticated caller; a team node belongs to its team only.
SELECT * FROM servers
WHERE team_id = $1 OR team_id IS NULL
ORDER BY created_at DESC, id DESC;

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

-- name: SetServerHostKey :one
-- Pins (or replaces) the TOFU host key fingerprint of a node.
UPDATE servers
SET host_key_fingerprint = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearServerHostKey :one
-- Forgets the pinned host key so the next validation re-pins it (operator reset
-- after a legitimate host key rotation).
UPDATE servers
SET host_key_fingerprint = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;
