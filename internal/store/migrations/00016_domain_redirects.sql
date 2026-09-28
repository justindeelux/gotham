-- +goose Up
-- Domain→domain redirect rules (BE-6.3).
--
-- A redirect rule is owned by one application (its node generates the Traefik
-- router/middleware pair; deleting the application cascades the rules away).
-- `source_domain` is the host the redirect answers for — it is globally
-- unique, must not shadow any application's base domain and must not equal
-- another enabled rule's target (the service rejects sequential conflicting
-- writes both ways; the generator additionally holds a racing chain's
-- later-created rule back as a diagnostic per committed snapshot). Domains are
-- stored normalized (lowercase, trimmed) and exact: wildcard sources are a
-- follow-up.
--
-- `code` records the operator's intent: 301 (permanent) or 302 (temporary).
-- Traefik's redirectRegex middleware only distinguishes permanent from
-- temporary and special-cases only GET, so GET answers 301 after a permanent
-- intent and 302 after a temporary one, while HEAD and every other method
-- answer 308/307 respectively. `preserve_path` keeps the request path and
-- query on the target; when false the redirect lands on the target root.
CREATE TABLE domain_redirects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    source_domain text NOT NULL,
    target_domain text NOT NULL,
    code smallint NOT NULL DEFAULT 301,
    preserve_path boolean NOT NULL DEFAULT true,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domain_redirects_code_check CHECK (code IN (301, 302)),
    CONSTRAINT domain_redirects_source_check CHECK (source_domain <> ''),
    CONSTRAINT domain_redirects_target_check CHECK (target_domain <> ''),
    CONSTRAINT domain_redirects_source_target_check CHECK (source_domain <> target_domain)
);

-- One rule per source host, enabled or not: disabling a rule pauses it without
-- releasing the host to another application.
CREATE UNIQUE INDEX domain_redirects_source_unique
    ON domain_redirects (source_domain);

CREATE INDEX domain_redirects_application_idx
    ON domain_redirects (application_id);

-- +goose Down
DROP TABLE IF EXISTS domain_redirects;
