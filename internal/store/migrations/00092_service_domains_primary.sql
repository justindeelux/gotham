-- +goose Up
-- Primary managed domain per service (GOTHAM-256).
--
-- service_domains mirrors application_domains: exactly one enabled row per
-- service is the primary, and the primary host is the one exposed as
-- GOTHAM_DOMAIN so a stable host no longer depends on which row was created
-- first. GOTHAM_DOMAINS keeps the same set of hosts.
--
-- The backfill promotes the oldest enabled row of every service, which is the
-- host GOTHAM_DOMAIN already rendered before this change. Services without an
-- enabled row stay without a primary, exactly like application_domains.
ALTER TABLE service_domains ADD COLUMN is_primary boolean NOT NULL DEFAULT false;

ALTER TABLE service_domains ADD CONSTRAINT service_domains_primary_enabled_check
    CHECK (NOT (is_primary AND disabled));

CREATE UNIQUE INDEX service_domains_service_primary_idx
    ON service_domains (service_id)
    WHERE is_primary;

UPDATE service_domains d
SET is_primary = true
FROM (
    SELECT DISTINCT ON (service_id) id
    FROM service_domains
    WHERE NOT disabled
    ORDER BY service_id, created_at, id
) keep
WHERE d.id = keep.id;

-- +goose Down
DROP INDEX IF EXISTS service_domains_service_primary_idx;
ALTER TABLE service_domains DROP CONSTRAINT IF EXISTS service_domains_primary_enabled_check;
ALTER TABLE service_domains DROP COLUMN IF EXISTS is_primary;
