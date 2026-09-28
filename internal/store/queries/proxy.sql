-- name: ListProxiedApplications :many
-- ProxiedApplications feeds the Traefik config generator (Phase 6, BE-6.1):
-- every application that declares a base_domain must be routed, on the node
-- that hosts it, from the endpoint of its newest running deployment. Rows are
-- ordered by creation time so duplicate-domain dispositions and generation
-- input stay deterministic.
SELECT a.id, a.server_id, a.base_domain, a.base_domain_disabled, a.port, a.host_port,
       COALESCE(d.container_id, '')::text AS container_id
FROM applications a
LEFT JOIN LATERAL (
    SELECT container_id FROM deployments
    WHERE application_id = a.id AND state = 'running' AND container_id <> ''
    ORDER BY created_at DESC
    LIMIT 1
) d ON true
WHERE a.base_domain <> ''
ORDER BY a.created_at, a.id;

-- name: NewestActiveProxyConfigVersion :one
-- NewestActiveProxyConfigVersion returns the configuration the node is
-- believed to serve: the newest promoted, not-yet-superseded version.
SELECT * FROM proxy_config_versions
WHERE server_id = $1 AND NOT pending AND superseded_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: PendingProxyConfigVersion :one
-- PendingProxyConfigVersion returns the newest version recorded but not yet
-- promoted (the intent to replace the active configuration).
SELECT * FROM proxy_config_versions
WHERE server_id = $1 AND pending
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: PreviousProxyConfigVersion :one
-- PreviousProxyConfigVersion returns the newest replaced predecessor.
SELECT * FROM proxy_config_versions
WHERE server_id = $1 AND NOT pending AND superseded_at IS NOT NULL
ORDER BY superseded_at DESC, id DESC
LIMIT 1;

-- name: InsertPendingProxyConfigVersion :one
-- InsertPendingProxyConfigVersion durably records a configuration about to be
-- pushed to the node, before the node is touched.
INSERT INTO proxy_config_versions (server_id, files, content_hash, pending)
VALUES ($1, $2, $3, true)
RETURNING *;

-- name: ClearPendingProxyConfigVersions :exec
-- ClearPendingProxyConfigVersions drops stale pending rows (a newer intent
-- replaces an older un-promoted one).
DELETE FROM proxy_config_versions WHERE server_id = $1 AND pending;

-- name: PromoteProxyConfigVersion :exec
-- PromoteProxyConfigVersion marks a pending version as the active one.
UPDATE proxy_config_versions SET pending = false WHERE id = $1;

-- name: SupersedeActiveProxyConfigVersions :exec
-- SupersedeActiveProxyConfigVersions retires the previous active version the
-- moment its replacement is promoted, starting its retention window.
UPDATE proxy_config_versions
SET superseded_at = now()
WHERE server_id = $1 AND NOT pending AND superseded_at IS NULL AND id <> $2;

-- name: PruneSupersededProxyConfigVersions :exec
-- PruneSupersededProxyConfigVersions drops predecessors whose retention
-- window has elapsed; the active version is never pruned.
DELETE FROM proxy_config_versions
WHERE server_id = $1 AND superseded_at IS NOT NULL AND superseded_at < $2;

-- name: DeleteProxyConfigVersion :exec
-- DeleteProxyConfigVersion drops one PENDING version (used to abort a record
-- after a push that provably never touched the node). The active snapshot can
-- never be deleted through this query.
DELETE FROM proxy_config_versions WHERE id = $1 AND server_id = $2 AND pending;
