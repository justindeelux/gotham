-- name: CreateServer :one
INSERT INTO servers (name, ip, port, ssh_user, ssh_key_id, team_id, encrypted_password)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateServer :one
-- Applies a PATCH edit computed by the domain service: every column is written
-- as given, so the service owns the merge (field-by-field COALESCE would hide
-- the difference between "unchanged" and "cleared to NULL"). host key and
-- status arrive already derived: an address/identity/auth change clears the pin
-- and returns the node to pending for revalidation.
UPDATE servers
SET name = $2,
    ip = $3,
    port = $4,
    ssh_user = $5,
    ssh_key_id = $6,
    encrypted_password = $7,
    host_key_fingerprint = $8,
    status = $9,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetServerByID :one
SELECT * FROM servers WHERE id = $1;

-- name: GetServerByIDForUpdate :one
-- Locks the node row for a PATCH edit (Update): the service re-reads under
-- this lock and computes the write from the locked row, so a concurrent
-- heartbeat/RegisterNode/ResetHostKey/Validate write cannot be reverted by a
-- stale read (JUS-5 fix round 1, defect 2).
SELECT * FROM servers WHERE id = $1 FOR UPDATE;

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

-- name: SetServerStatusAfterValidation :one
-- Restores a node's status after a successful SSH validation. Ready is reserved
-- for a live agent heartbeat, so the status is derived from last_seen at the
-- moment of the write: a heartbeat that landed during the validation (fresh
-- last_seen) keeps the node ready, a node seen before but now past the window
-- goes offline, and a node never seen stays pending. Deriving it in one
-- statement means a concurrent heartbeat cannot be clobbered by a stale read
-- (A4-15/B4-9, fix round 1 U2).
UPDATE servers
SET status = CASE
        WHEN last_seen IS NOT NULL AND last_seen >= $2 THEN 'ready'
        WHEN last_seen IS NOT NULL THEN 'offline'
        ELSE 'pending'
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetServerStatusAfterValidationGuarded :one
-- Restores the heartbeat-derived status like SetServerStatusAfterValidation,
-- but only when the row still matches the endpoint and credentials the
-- validation ran against (JUS-5 fix round 1, defect 1). A concurrent PATCH
-- that moved the node to a new address (and back to pending) is left alone;
-- 0 rows means the endpoint moved on and the caller drops the write. A
-- heartbeat during the validation touches none of the guarded columns, so the
-- live-agent derivation still lands.
UPDATE servers
SET status = CASE
        WHEN last_seen IS NOT NULL AND last_seen >= $2 THEN 'ready'
        WHEN last_seen IS NOT NULL THEN 'offline'
        ELSE 'pending'
    END,
    updated_at = now()
WHERE id = $1
  AND ip = $3
  AND port = $4
  AND ssh_user = $5
  AND ssh_key_id IS NOT DISTINCT FROM $6
  AND encrypted_password IS NOT DISTINCT FROM $7
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

-- name: PinServerHostKeyGuarded :one
-- Pins like PinServerHostKey, but only when the row still matches the endpoint
-- and credentials the validation ran against (JUS-5 fix round 1, defect 1). A
-- concurrent PATCH that changed the address or credentials clears the pin and
-- returns the node to pending; the stale validation's pin then affects 0 rows
-- and must be dropped instead of pinning the OLD host's key onto the NEW
-- address. A heartbeat or inventory write touches none of the guarded columns,
-- so it never blocks a legitimate pin.
UPDATE servers
SET host_key_fingerprint = $2,
    updated_at = now()
WHERE id = $1 AND host_key_fingerprint IS NULL
  AND ip = $3
  AND port = $4
  AND ssh_user = $5
  AND ssh_key_id IS NOT DISTINCT FROM $6
  AND encrypted_password IS NOT DISTINCT FROM $7
RETURNING *;

-- name: SetServerStatusGuarded :one
-- Sets the status only when the row still matches the endpoint and credentials
-- the caller acted on (JUS-5 fix round 1, defect 1): a stale validation must
-- not overwrite the pending status a concurrent PATCH set. 0 rows means the
-- endpoint moved on; the caller drops the write.
UPDATE servers
SET status = $2,
    updated_at = now()
WHERE id = $1
  AND ip = $3
  AND port = $4
  AND ssh_user = $5
  AND ssh_key_id IS NOT DISTINCT FROM $6
  AND encrypted_password IS NOT DISTINCT FROM $7
RETURNING *;

-- name: UpdateServerAgentInfoGuarded :one
-- Records validation inventory only when the row still matches the endpoint
-- the validation ran against (JUS-5 fix round 1, defect 1): a concurrent PATCH
-- to a new address must not inherit the OLD host's inventory. Like the
-- unguarded variant it never touches status or last_seen.
UPDATE servers
SET node_id = $2,
    os = $3,
    docker_version = $4,
    arch = $5,
    total_mem = $6,
    total_disk = $7,
    updated_at = now()
WHERE id = $1
  AND ip = $8
  AND port = $9
  AND ssh_user = $10
  AND ssh_key_id IS NOT DISTINCT FROM $11
  AND encrypted_password IS NOT DISTINCT FROM $12
RETURNING *;

-- name: ClearServerHostKey :one
-- Forgets the pinned host key so the next validation re-pins it (operator reset
-- after a legitimate host key rotation).
UPDATE servers
SET host_key_fingerprint = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;
