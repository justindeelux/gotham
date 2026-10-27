-- name: UpsertNodeObservation :one
-- Stores the latest accepted generation. The WHERE guard makes duplicate and
-- out-of-order reports a no-op: within one session the sequence must increase;
-- a new session (agent restart/reconnect) must carry a newer observation, so an
-- old session can never overwrite fresher state. A partial inventory updates
-- host/filesystem evidence but preserves the last complete inventory generation.
INSERT INTO node_observations (
    server_id,
    session_id,
    sequence,
    observed_at,
    received_at,
    sampling_interval_ms,
    agent_version,
    host,
    filesystems,
    inventory,
    inventory_complete,
    inventory_reason,
    inventory_observed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (server_id) DO UPDATE
SET session_id = EXCLUDED.session_id,
    sequence = EXCLUDED.sequence,
    observed_at = EXCLUDED.observed_at,
    received_at = EXCLUDED.received_at,
    sampling_interval_ms = EXCLUDED.sampling_interval_ms,
    agent_version = EXCLUDED.agent_version,
    host = EXCLUDED.host,
    filesystems = EXCLUDED.filesystems,
    inventory = CASE
        WHEN EXCLUDED.inventory_complete THEN EXCLUDED.inventory
        ELSE node_observations.inventory
    END,
    inventory_complete = CASE
        WHEN EXCLUDED.inventory_complete THEN true
        ELSE node_observations.inventory_complete
    END,
    inventory_reason = CASE
        WHEN EXCLUDED.inventory_complete THEN ''
        ELSE node_observations.inventory_reason
    END,
    inventory_observed_at = CASE
        WHEN EXCLUDED.inventory_complete THEN EXCLUDED.inventory_observed_at
        ELSE node_observations.inventory_observed_at
    END,
    updated_at = now()
WHERE (
        node_observations.session_id = EXCLUDED.session_id
        AND node_observations.sequence < EXCLUDED.sequence
    )
    OR (
        node_observations.session_id <> EXCLUDED.session_id
        AND node_observations.observed_at < EXCLUDED.observed_at
    )
RETURNING *;

-- name: GetNodeObservation :one
SELECT * FROM node_observations WHERE server_id = $1;

-- name: DeleteNodeObservation :exec
DELETE FROM node_observations WHERE server_id = $1;
