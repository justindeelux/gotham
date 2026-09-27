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

-- name: LatestProxyConfigVersions :many
-- LatestProxyConfigVersions returns a node's pushed configuration versions,
-- newest first. Revert uses the second entry when two exist.
SELECT * FROM proxy_config_versions
WHERE server_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: InsertProxyConfigVersion :one
-- InsertProxyConfigVersion records one successfully pushed configuration.
INSERT INTO proxy_config_versions (server_id, files, content_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: PruneProxyConfigVersions :exec
-- PruneProxyConfigVersions drops versions older than the retention window.
DELETE FROM proxy_config_versions
WHERE server_id = $1 AND created_at < $2;
