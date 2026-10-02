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
-- Records the capabilities an SSH validation or agent registration reported. It
-- deliberately does NOT touch status or last_seen: ready means a live agent
-- heartbeat, so SSH reachability or a one-off registration must never flip a
-- node ready on its own (A4-15/B4-9).
UPDATE servers
SET node_id = $2,
    os = $3,
    docker_version = $4,
    arch = $5,
    total_mem = $6,
    total_disk = $7,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpsertServerByNodeID :one
-- Conflict-safe registration keyed on the node's stable identity. A concurrent
-- registration of the same node id converges on one row: the winner inserts and
-- the loser's ON CONFLICT updates that same row, so a race can never leave a
-- duplicate or an orphan row with a NULL node_id (A4-12). name/ip are not
-- overwritten on conflict so an operator's label survives re-registration.
INSERT INTO servers (name, ip, port, ssh_user, node_id, os, docker_version, arch, total_mem, total_disk)
VALUES ($1, $2, $3, '', $4, $5, $6, $7, $8, $9)
ON CONFLICT (node_id) DO UPDATE
SET os = EXCLUDED.os,
    docker_version = EXCLUDED.docker_version,
    arch = EXCLUDED.arch,
    total_mem = EXCLUDED.total_mem,
    total_disk = EXCLUDED.total_disk,
    updated_at = now()
RETURNING *;

-- name: ClaimServerByNodeID :one
-- Claims an operator-created server row (node_id still NULL) whose address
-- matches the registering node, so enrollment updates that existing row instead
-- of inserting a duplicate. The row lock serializes two concurrent
-- registrations: the loser's subquery re-reads the claimed row, finds no
-- candidate, and falls back to the node-id upsert.
UPDATE servers
SET node_id = $1,
    updated_at = now()
WHERE id = (
    SELECT s.id FROM servers s
    WHERE s.node_id IS NULL AND s.ip <> '' AND s.ip = $2
    ORDER BY s.created_at ASC
    LIMIT 1
    FOR UPDATE
)
RETURNING *;

-- name: MarkStaleServersOffline :exec
-- Flips nodes that were ready but have not heartbeated since the cutoff to
-- offline, so the read path always reports the live status the FE understands
-- (A4-6). A ready row with no last_seen is stale by definition.
UPDATE servers
SET status = 'offline',
    updated_at = now()
WHERE status = 'ready' AND (last_seen IS NULL OR last_seen < $1);

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

-- name: PinServerHostKey :one
-- Pins the TOFU host key fingerprint of a node only when it is still unpinned.
-- A stale first-use validation then cannot overwrite a pin written by a racing
-- validation; 0 rows means the node was pinned in the meantime and the caller
-- must re-read and fail closed on a mismatch.
UPDATE servers
SET host_key_fingerprint = $2,
    updated_at = now()
WHERE id = $1 AND host_key_fingerprint IS NULL
RETURNING *;

-- name: ClearServerHostKey :one
-- Forgets the pinned host key so the next validation re-pins it (operator reset
-- after a legitimate host key rotation).
UPDATE servers
SET host_key_fingerprint = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;
