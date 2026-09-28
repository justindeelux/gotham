-- name: ListProxiedApplications :many
-- ProxiedApplications feeds the Traefik config generator (Phase 6, BE-6.1):
-- every application that declares a base_domain must be routed, on the node
-- that hosts it, from the endpoint of its newest running deployment. Rows are
-- ordered by creation time so duplicate-domain dispositions and generation
-- input stay deterministic.
--
-- The certificate columns (BE-6.2) carry the per-application certificate
-- intent. They are NULL-joined as empty values: certificate_configured tells
-- the generator whether an intent exists at all, and the recorded domain
-- lets it refuse to activate a certificate whose host no longer matches.
SELECT a.id, a.server_id, a.base_domain, a.base_domain_disabled, a.port, a.host_port,
       COALESCE(d.container_id, '')::text AS container_id,
       (c.id IS NOT NULL)::boolean AS certificate_configured,
       COALESCE(c.domain, '')::text AS certificate_domain,
       COALESCE(c.enabled, false) AS certificate_enabled,
       COALESCE(c.challenge, '')::text AS certificate_challenge,
       COALESCE(c.wildcard, false) AS certificate_wildcard,
       c.dns_provider_id AS certificate_dns_provider_id
FROM applications a
LEFT JOIN domain_certificates c ON c.application_id = a.id
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

-- name: ListDNSProviders :many
-- ListDNSProviders returns every configured DNS provider, newest first. The
-- sealed credential is read by the proxy service to build the Traefik
-- container environment; it is never returned through the API.
SELECT * FROM dns_providers
ORDER BY created_at DESC, id DESC;

-- name: GetDNSProvider :one
SELECT * FROM dns_providers WHERE id = $1;

-- name: CreateDNSProvider :one
INSERT INTO dns_providers (provider, name, zones, ciphertext, enabled)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateDNSProvider :one
UPDATE dns_providers
SET provider = $2,
    name = $3,
    zones = $4,
    ciphertext = $5,
    enabled = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateDNSProviderMeta :one
-- UpdateDNSProviderMeta writes everything but the sealed credential, so an
-- update that does not rotate can never restore an older ciphertext after a
-- concurrent rotation.
UPDATE dns_providers
SET provider = $2,
    name = $3,
    zones = $4,
    enabled = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteDNSProvider :exec
DELETE FROM dns_providers WHERE id = $1;

-- name: CountEnabledDomainCertificatesByProvider :one
-- CountEnabledDomainCertificatesByProvider guards provider disable/delete/type
-- changes: an enabled certificate config must never lose its provider
-- silently.
SELECT count(*) FROM domain_certificates
WHERE dns_provider_id = $1 AND enabled;

-- name: CountDomainCertificatesByProvider :one
-- CountDomainCertificatesByProvider guards provider deletion, which the
-- foreign key would otherwise reject after the fact.
SELECT count(*) FROM domain_certificates
WHERE dns_provider_id = $1;

-- name: ListEnabledDomainCertificatesByProvider :many
-- ListEnabledDomainCertificatesByProvider feeds the zone-narrowing guard.
SELECT * FROM domain_certificates
WHERE dns_provider_id = $1 AND enabled
ORDER BY created_at, id;

-- name: ListDomainCertificates :many
SELECT * FROM domain_certificates
ORDER BY created_at DESC, id DESC;

-- name: GetDomainCertificate :one
SELECT * FROM domain_certificates WHERE id = $1;

-- name: GetDomainCertificateByApplication :one
SELECT * FROM domain_certificates WHERE application_id = $1;

-- name: CreateDomainCertificate :one
INSERT INTO domain_certificates (application_id, domain, enabled, challenge, dns_provider_id, wildcard)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateDomainCertificate :one
UPDATE domain_certificates
SET domain = $2,
    enabled = $3,
    challenge = $4,
    dns_provider_id = $5,
    wildcard = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteDomainCertificate :exec
DELETE FROM domain_certificates WHERE id = $1;

-- name: ListDomainRedirects :many
-- ListDomainRedirects returns every redirect rule, newest first.
SELECT * FROM domain_redirects
ORDER BY created_at DESC, id DESC;

-- name: ListDomainRedirectsByApplication :many
-- ListDomainRedirectsByApplication returns one application's rules, newest
-- first.
SELECT * FROM domain_redirects
WHERE application_id = $1
ORDER BY created_at DESC, id DESC;

-- name: GetDomainRedirect :one
SELECT * FROM domain_redirects WHERE id = $1;

-- name: GetDomainRedirectBySource :one
-- GetDomainRedirectBySource resolves the unique source-host claim.
SELECT * FROM domain_redirects WHERE source_domain = $1;

-- name: CreateDomainRedirect :one
INSERT INTO domain_redirects (application_id, source_domain, target_domain, code, preserve_path, enabled)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateDomainRedirect :one
UPDATE domain_redirects
SET source_domain = $2,
    target_domain = $3,
    code = $4,
    preserve_path = $5,
    enabled = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteDomainRedirect :exec
DELETE FROM domain_redirects WHERE id = $1;

-- name: ListApplicationBaseDomains :many
-- ListApplicationBaseDomains feeds the redirect ownership guard: a redirect
-- source must never shadow any application's base domain (on any node), so
-- the two routers can never match the same host.
SELECT id, base_domain FROM applications WHERE base_domain <> '';

-- name: ListEnabledRedirectSources :many
-- ListEnabledRedirectSources feeds the no-chain guard: a redirect target must
-- not equal another enabled rule's source.
SELECT id, source_domain FROM domain_redirects WHERE enabled;

-- name: ListRedirectRules :many
-- ListRedirectRules joins every redirect rule to its application's node state
-- for the proxy generator: the owning node decides which Traefik instance
-- serves the rule, and the domain-disabled flag holds back rules of an
-- application whose route is already excluded by the uniqueness conflict.
SELECT r.id, r.application_id, r.source_domain, r.target_domain, r.code,
       r.preserve_path, r.enabled, r.created_at, r.updated_at,
       a.server_id, a.base_domain_disabled
FROM domain_redirects r
JOIN applications a ON a.id = r.application_id
ORDER BY r.created_at, r.id;

-- name: ListCertificateStatusTargets :many
-- ListCertificateStatusTargets joins every certificate intent to the node
-- hosting its application, so the status service can read each node's ACME
-- storage once and map certificates to it. Disabled intents are included:
-- status is a fact about the node's storage, not about the intent.
SELECT c.id AS certificate_id, c.domain, a.server_id
FROM domain_certificates c
JOIN applications a ON a.id = c.application_id
ORDER BY c.id;
