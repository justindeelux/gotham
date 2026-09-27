-- name: ListProxiedApplications :many
-- ProxiedApplications feeds the Traefik config generator (Phase 6, BE-6.1):
-- every application that declares a base_domain must be routed, on the node
-- that hosts it. Ordered by id so generation input is deterministic.
SELECT * FROM applications WHERE base_domain <> '' ORDER BY id;
