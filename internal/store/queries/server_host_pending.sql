-- name: GetServerHostPending :one
SELECT * FROM server_host_pending WHERE server_id = $1;

-- name: BeginServerHostPending :execrows
-- Refuses a second pending change on the same node: the primary key conflict
-- leaves the existing row (and its revert countdown) untouched.
INSERT INTO server_host_pending (server_id, pending, deadline)
VALUES ($1, $2, $3)
ON CONFLICT (server_id) DO NOTHING;

-- name: DeleteServerHostPending :exec
DELETE FROM server_host_pending WHERE server_id = $1;
