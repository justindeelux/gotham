-- +goose Up
-- Managed domains for compose services (GOTHAM-123).
--
-- A service used to be routable only through the gotham.domain compose labels
-- it was created with, which forced the user to own a domain before the
-- service existed. service_domains mirrors application_domains: every row is
-- one host routed to one compose service and container port, with an HTTP or
-- HTTPS intent and a disable flag. The labels stay supported (compose import,
-- existing services) and remain the read-only "declared in compose" routes.
--
-- Enabled hosts are unique platform-wide and case-insensitively. A disabled
-- row keeps its claim paused, like application_domains. Cross-table claims
-- (application domains, redirect sources, compose labels) are enforced by the
-- service write path, which serialises on the host.
CREATE TABLE service_domains (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id uuid NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    compose_service text NOT NULL,
    port integer NOT NULL,
    domain text NOT NULL,
    protocol text NOT NULL DEFAULT 'http',
    disabled boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT service_domains_port_check CHECK (port >= 1 AND port <= 65535),
    CONSTRAINT service_domains_domain_check CHECK (domain <> ''),
    CONSTRAINT service_domains_protocol_check CHECK (protocol IN ('http', 'https'))
);

CREATE UNIQUE INDEX service_domains_service_domain_idx
    ON service_domains (service_id, lower(domain));

CREATE UNIQUE INDEX service_domains_global_domain_idx
    ON service_domains (lower(domain))
    WHERE NOT disabled;

CREATE INDEX service_domains_service_idx
    ON service_domains (service_id, created_at, id);

-- The host set a successful deploy rendered, so the service JSON can report
-- pending_changes=["domains"] until the next deploy picks up a new set.
ALTER TABLE service_deploys ADD COLUMN domains text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE service_deploys DROP COLUMN IF EXISTS domains;
DROP TABLE IF EXISTS service_domains;
