-- Host pressure policy and filesystem history (GOTHAM-84).

-- name: GetInstanceMonitoringPolicy :one
SELECT * FROM monitoring_policies
WHERE scope = 'instance' AND node_id IS NULL;

-- name: GetNodeMonitoringPolicy :one
SELECT * FROM monitoring_policies
WHERE scope = 'node' AND node_id = $1;

-- name: UpsertInstanceMonitoringPolicy :one
INSERT INTO monitoring_policies (id, scope, node_id, rules, updated_by, updated_at)
VALUES ($1, 'instance', NULL, $2, $3, $4)
ON CONFLICT ((scope)) WHERE scope = 'instance'
DO UPDATE SET rules = EXCLUDED.rules,
              updated_by = EXCLUDED.updated_by,
              updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: UpsertNodeMonitoringPolicy :one
INSERT INTO monitoring_policies (id, scope, node_id, rules, updated_by, updated_at)
VALUES ($1, 'node', $2, $3, $4, $5)
ON CONFLICT (node_id) WHERE scope = 'node'
DO UPDATE SET rules = EXCLUDED.rules,
              updated_by = EXCLUDED.updated_by,
              updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: DeleteInstanceMonitoringPolicy :execrows
DELETE FROM monitoring_policies WHERE scope = 'instance' AND node_id IS NULL;

-- name: DeleteNodeMonitoringPolicy :execrows
DELETE FROM monitoring_policies WHERE scope = 'node' AND node_id = $1;

-- name: GetFilesystemIdentity :one
SELECT * FROM filesystem_identities
WHERE node_id = $1 AND filesystem_key = $2;

-- name: CreateFilesystemIdentity :one
INSERT INTO filesystem_identities (
    id, node_id, filesystem_key, path, purpose, segment, total_bytes,
    total_inodes, first_seen_at, last_seen_at
) VALUES ($1, $2, $3, $4, $5, 1, $6, $7, $8, $8)
RETURNING *;

-- name: UpdateFilesystemIdentity :one
UPDATE filesystem_identities
SET path = $1,
    purpose = $2,
    segment = $3,
    total_bytes = $4,
    total_inodes = $5,
    last_seen_at = $6
WHERE id = $7
RETURNING *;

-- name: ListFilesystemIdentitiesByNode :many
SELECT * FROM filesystem_identities WHERE node_id = $1 ORDER BY path;

-- name: UpsertFilesystemUsageBucket :exec
INSERT INTO filesystem_usage_buckets (
    identity_id, segment, bucket_at, samples, fresh_samples, partial_samples,
    unknown_samples, min_used_bytes, max_used_bytes, last_used_bytes,
    min_used_inodes, max_used_inodes, last_used_inodes, total_bytes,
    total_inodes, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT (identity_id, segment, bucket_at) DO UPDATE SET
    samples = filesystem_usage_buckets.samples + EXCLUDED.samples,
    fresh_samples = filesystem_usage_buckets.fresh_samples + EXCLUDED.fresh_samples,
    partial_samples = filesystem_usage_buckets.partial_samples + EXCLUDED.partial_samples,
    unknown_samples = filesystem_usage_buckets.unknown_samples + EXCLUDED.unknown_samples,
    min_used_bytes = CASE
        WHEN EXCLUDED.min_used_bytes IS NULL THEN filesystem_usage_buckets.min_used_bytes
        WHEN filesystem_usage_buckets.min_used_bytes IS NULL THEN EXCLUDED.min_used_bytes
        ELSE LEAST(filesystem_usage_buckets.min_used_bytes, EXCLUDED.min_used_bytes)
    END,
    max_used_bytes = CASE
        WHEN EXCLUDED.max_used_bytes IS NULL THEN filesystem_usage_buckets.max_used_bytes
        WHEN filesystem_usage_buckets.max_used_bytes IS NULL THEN EXCLUDED.max_used_bytes
        ELSE GREATEST(filesystem_usage_buckets.max_used_bytes, EXCLUDED.max_used_bytes)
    END,
    last_used_bytes = COALESCE(EXCLUDED.last_used_bytes, filesystem_usage_buckets.last_used_bytes),
    min_used_inodes = CASE
        WHEN EXCLUDED.min_used_inodes IS NULL THEN filesystem_usage_buckets.min_used_inodes
        WHEN filesystem_usage_buckets.min_used_inodes IS NULL THEN EXCLUDED.min_used_inodes
        ELSE LEAST(filesystem_usage_buckets.min_used_inodes, EXCLUDED.min_used_inodes)
    END,
    max_used_inodes = CASE
        WHEN EXCLUDED.max_used_inodes IS NULL THEN filesystem_usage_buckets.max_used_inodes
        WHEN filesystem_usage_buckets.max_used_inodes IS NULL THEN EXCLUDED.max_used_inodes
        ELSE GREATEST(filesystem_usage_buckets.max_used_inodes, EXCLUDED.max_used_inodes)
    END,
    last_used_inodes = COALESCE(EXCLUDED.last_used_inodes, filesystem_usage_buckets.last_used_inodes),
    total_bytes = EXCLUDED.total_bytes,
    total_inodes = EXCLUDED.total_inodes,
    updated_at = EXCLUDED.updated_at;

-- name: ListFilesystemIdentities :many
SELECT * FROM filesystem_identities ORDER BY node_id, path;

-- name: ListFilesystemUsageBuckets :many
-- Forecasting reads only the current segment and the bounded window, never the
-- raw samples behind a bucket.
SELECT * FROM filesystem_usage_buckets
WHERE identity_id = $1
  AND segment = $2
  AND bucket_at >= $3
ORDER BY bucket_at;

-- name: DeleteFilesystemUsageBefore :execrows
DELETE FROM filesystem_usage_buckets WHERE bucket_at < $1;
