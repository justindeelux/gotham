-- +goose Up
-- Multiple domains per application (JUS-89).
--
-- application_domains is the one-to-many domain table: an application serves
-- every non-disabled row, exactly one of which is the primary
-- (applications.base_domain mirrors it and stays the read-compatible source
-- for every caller that only needs one host). The previous model kept a
-- single base_domain column per application, which cannot express aliases
-- such as example.com + www.example.com.
--
-- Uniqueness is platform-wide and case-insensitive: the Traefik generator
-- holds back every binding of a duplicate host, and two applications on
-- different nodes serving the same hostname would both answer it, so the
-- write path rejects a host any other application already claims (a disabled
-- row keeps its claim paused, mirroring the redirect-source convention: an
-- operator re-enabling it must still resolve the conflict explicitly).
-- One primary per application is a partial unique index.
--
-- The backfill is data-safe: every stored base_domain becomes the primary
-- row, and when several applications claim the same host (allowed across
-- nodes before this change) only the oldest binding stays enabled — row age
-- does not prove ownership either, but a single routable binding must exist
-- for the node to converge, so the newest conflicting bindings fail closed
-- (disabled, never deleted or reassigned) exactly like migration 00012.
-- Rows backfilled from a migration-disabled base_domain stay disabled.
CREATE TABLE application_domains (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    domain text NOT NULL,
    is_primary boolean NOT NULL DEFAULT false,
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT application_domains_domain_check CHECK (domain <> '')
);

CREATE UNIQUE INDEX application_domains_app_domain_idx
    ON application_domains (application_id, lower(domain));

CREATE UNIQUE INDEX application_domains_global_domain_idx
    ON application_domains (lower(domain))
    WHERE NOT disabled;

CREATE UNIQUE INDEX application_domains_app_primary_idx
    ON application_domains (application_id)
    WHERE is_primary;

INSERT INTO application_domains (application_id, domain, is_primary, disabled, created_at)
SELECT id, lower(base_domain), true, true, created_at
FROM applications
WHERE base_domain <> '';

UPDATE application_domains d
SET disabled = false
FROM (
    SELECT DISTINCT ON (lower(d2.domain)) d2.id
    FROM application_domains d2
    JOIN applications a ON a.id = d2.application_id
    WHERE NOT a.base_domain_disabled
    ORDER BY lower(d2.domain), d2.created_at, d2.id
) keep
WHERE d.id = keep.id;

-- +goose Down
DROP TABLE IF EXISTS application_domains;
