-- name: ListApplicationDomainsByApplication :many
-- ListApplicationDomainsByApplication returns one application's domains, the
-- primary first, then oldest first. The order is the routing and promotion
-- order: generation names the primary router after the application, and
-- removing the primary promotes the next row deterministically (JUS-89).
SELECT * FROM application_domains
WHERE application_id = $1
ORDER BY is_primary DESC, created_at, id;

-- name: GetApplicationDomainByNameAnyApp :one
-- GetApplicationDomainByNameAnyApp resolves the platform-wide claim on a
-- host for the uniqueness guard (JUS-89): any application's row, including
-- disabled ones, keeps its claim paused rather than released.
SELECT * FROM application_domains
WHERE lower(domain) = lower($1)
ORDER BY created_at, id
LIMIT 1;

-- name: CreateApplicationDomain :one
INSERT INTO application_domains (application_id, domain, is_primary, disabled)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateApplicationDomain :one
UPDATE application_domains
SET domain = $2,
    is_primary = $3,
    disabled = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearPrimaryApplicationDomains :exec
-- ClearPrimaryApplicationDomains drops the primary flag of an application
-- before a promotion, so the partial unique index never sees two primaries.
UPDATE application_domains
SET is_primary = false, updated_at = now()
WHERE application_id = $1 AND is_primary;

-- name: DeleteApplicationDomain :exec
DELETE FROM application_domains WHERE id = $1 AND application_id = $2;

-- name: DeleteApplicationDomainsByApplication :exec
DELETE FROM application_domains WHERE application_id = $1;

-- name: ListProxiedApplicationDomains :many
-- ListProxiedApplicationDomains feeds the Traefik generator (JUS-89): every
-- domain row joined to its application's node, ordered deterministically so
-- generation input is stable. Disabled rows are included with their flag so
-- the generator can report them as diagnostics instead of silently dropping
-- them.
SELECT d.application_id, a.server_id, d.domain, d.is_primary, d.disabled, d.created_at, d.id
FROM application_domains d
JOIN applications a ON a.id = d.application_id
ORDER BY a.created_at, a.id, d.is_primary DESC, d.created_at, d.id;
