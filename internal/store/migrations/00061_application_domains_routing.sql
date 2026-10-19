-- +goose Up
-- Decouple an application from its routing shape (JUS-233). An application is
-- no longer tied to one port/domain: every domain row carries the target
-- container port, the optional compose service that serves it and the
-- protocol Traefik must use. An application may have zero domains, in which
-- case it deploys without a published port and generates no route.
--
-- DB reset is accepted for this change, so the legacy app-level routing
-- columns are dropped without a backfill. Uniqueness rules on
-- application_domains (same application + host, platform-wide enabled host,
-- one primary per application) are unchanged.
ALTER TABLE application_domains
    ADD COLUMN port integer NOT NULL DEFAULT 0,
    ADD COLUMN service text,
    ADD COLUMN protocol text NOT NULL DEFAULT 'http',
    ADD CONSTRAINT application_domains_port_check CHECK (port >= 0 AND port <= 65535),
    ADD CONSTRAINT application_domains_protocol_check CHECK (protocol IN ('http', 'https'));

ALTER TABLE applications
    DROP COLUMN base_domain,
    DROP COLUMN base_domain_disabled,
    DROP COLUMN port,
    DROP COLUMN host_port;

-- +goose Down
ALTER TABLE applications
    ADD COLUMN base_domain text NOT NULL DEFAULT '',
    ADD COLUMN base_domain_disabled boolean NOT NULL DEFAULT false,
    ADD COLUMN port integer NOT NULL DEFAULT 0,
    ADD COLUMN host_port integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT applications_port_check CHECK (port >= 0 AND port <= 65535),
    ADD CONSTRAINT applications_host_port_check CHECK (host_port >= 0 AND host_port <= 65535);

ALTER TABLE application_domains
    DROP CONSTRAINT application_domains_port_check,
    DROP CONSTRAINT application_domains_protocol_check,
    DROP COLUMN port,
    DROP COLUMN service,
    DROP COLUMN protocol;
