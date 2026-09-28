-- +goose Up
-- SSL certificate configuration (BE-6.2).
--
-- dns_providers holds the DNS-01 credentials of the certificate resolvers
-- Traefik runs. `provider` is restricted to the allowlist the generator knows
-- (lego, the library behind Traefik's dnsChallenge, has one credential
-- environment variable per provider type, so at most one ENABLED row per type
-- may exist — enforced by the partial unique index). `zones` lists the DNS
-- zones the credential may write challenge records into; `ciphertext` is the
-- API token sealed with AES-256-GCM (providers.SealSecret). Plaintext never
-- reaches this table, the API, the generated configuration or the logs.
CREATE TABLE dns_providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL,
    name text NOT NULL DEFAULT '',
    zones text[] NOT NULL,
    ciphertext text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dns_providers_provider_check CHECK (
        provider IN ('cloudflare', 'digitalocean')
    ),
    CONSTRAINT dns_providers_zones_check CHECK (cardinality(zones) > 0),
    CONSTRAINT dns_providers_ciphertext_check CHECK (ciphertext <> '')
);

CREATE UNIQUE INDEX dns_providers_enabled_type_idx
    ON dns_providers (provider)
    WHERE enabled;

-- domain_certificates is the per-application certificate intent: the operator
-- decides how the domain is certified, and the generator activates the HTTPS
-- router + HTTP→HTTPS redirect for a route only when an enabled row exists
-- whose recorded domain still matches the application's current base_domain.
-- `domain` records the host the certificate was configured for; a domain
-- change through the deploy API leaves the row stale until the next
-- certificate update re-records it (the generator surfaces the divergence as
-- a diagnostic instead of issuing for the stale host).
--
-- challenge http-01 uses the shared default resolver; dns-01 requires an
-- enabled DNS provider (wildcard certificates are dns-01 only).
CREATE TABLE domain_certificates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    domain text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    challenge text NOT NULL DEFAULT 'http-01',
    dns_provider_id uuid REFERENCES dns_providers(id) ON DELETE RESTRICT,
    wildcard boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domain_certificates_challenge_check CHECK (
        challenge IN ('http-01', 'dns-01')
    ),
    CONSTRAINT domain_certificates_dns_provider_check CHECK (
        (challenge = 'dns-01' AND dns_provider_id IS NOT NULL) OR
        (challenge = 'http-01' AND dns_provider_id IS NULL)
    ),
    CONSTRAINT domain_certificates_wildcard_check CHECK (
        NOT wildcard OR challenge = 'dns-01'
    ),
    CONSTRAINT domain_certificates_application_unique UNIQUE (application_id)
);

CREATE INDEX domain_certificates_provider_idx
    ON domain_certificates (dns_provider_id);

-- +goose Down
DROP TABLE IF EXISTS domain_certificates;
DROP TABLE IF EXISTS dns_providers;
