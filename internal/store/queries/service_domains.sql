-- name: ListServiceDomainsByService :many
-- Every managed domain of one service, oldest first. The order is the routing
-- and GOTHAM_DOMAIN order: the first enabled row is the primary host.
SELECT * FROM service_domains
WHERE service_id = $1
ORDER BY created_at, id;

-- name: GetServiceDomainByNameAnyService :one
-- Resolves the platform-wide claim on a host for the uniqueness guard: any
-- service's row, including disabled ones, keeps its claim paused.
SELECT * FROM service_domains
WHERE lower(domain) = lower($1)
ORDER BY created_at, id
LIMIT 1;

-- name: CreateServiceDomain :one
INSERT INTO service_domains (id, service_id, compose_service, port, domain, protocol, disabled)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateServiceDomain :one
UPDATE service_domains
SET compose_service = $3,
    port = $4,
    domain = $5,
    protocol = $6,
    disabled = $7,
    updated_at = now()
WHERE id = $1 AND service_id = $2
RETURNING *;

-- name: DeleteServiceDomain :exec
DELETE FROM service_domains WHERE id = $1 AND service_id = $2;

-- name: DeleteServiceDomainsByService :exec
-- A soft-deleted service keeps its row, so the FK cascade alone never fires on
-- the normal delete path; this drops the managed domains explicitly.
DELETE FROM service_domains WHERE service_id = $1;

-- name: ListProxiedServiceDomains :many
-- Feeds the Traefik generator: every service domain joined to its node,
-- ordered deterministically. Disabled rows are omitted because they generate
-- no route; the API lists them separately with their diagnostic.
SELECT d.service_id, s.server_id, d.compose_service, d.port, d.domain,
       d.protocol, d.disabled
FROM service_domains d
JOIN services s ON s.id = d.service_id
WHERE s.deleted_at IS NULL AND NOT d.disabled
ORDER BY s.created_at, s.id, d.created_at, d.id;
