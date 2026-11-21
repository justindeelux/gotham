-- +goose Up
-- Certificate intents for service domains (GOTHAM-257).
--
-- A certificate intent belongs to exactly one routing owner: an application
-- (application_id), a service (service_id), or the control plane (neither).
-- Existing rows keep their application_id and are unaffected; service_id is
-- null for all of them. The generated owner_kind gains the service value.
--
-- The panel uniqueness index must exclude service rows: they are not panel
-- hosts, and two owners never share a host anyway (the domain claim guard).
ALTER TABLE domain_certificates
    ADD COLUMN service_id uuid REFERENCES services(id) ON DELETE CASCADE;

ALTER TABLE domain_certificates
    ADD CONSTRAINT domain_certificates_owner_check
    CHECK (application_id IS NULL OR service_id IS NULL);

ALTER TABLE domain_certificates
    ADD CONSTRAINT domain_certificates_service_domain_unique UNIQUE (service_id, domain);

DROP INDEX IF EXISTS domain_certificates_panel_domain_unique;
CREATE UNIQUE INDEX domain_certificates_panel_domain_unique
    ON domain_certificates (domain)
    WHERE application_id IS NULL AND service_id IS NULL;

ALTER TABLE domain_certificates DROP COLUMN owner_kind;
ALTER TABLE domain_certificates ADD COLUMN owner_kind text GENERATED ALWAYS AS (
    CASE
        WHEN application_id IS NULL AND service_id IS NULL THEN 'control_plane'
        WHEN service_id IS NOT NULL THEN 'service'
        ELSE 'application'
    END
) STORED;

CREATE INDEX domain_certificates_service_idx
    ON domain_certificates (service_id);

-- Backfill (GOTHAM-257): every existing https service domain gets the same
-- default http-01 intent the write path now records. Without it, adding a
-- second https domain or configuring a certificate on one row would make the
-- other legacy hosts resolve as "recorded for a different domain" and fall
-- back to plain HTTP. Soft-deleted services are skipped: their intents have no
-- routable owner.
INSERT INTO domain_certificates (service_id, domain, enabled, challenge, wildcard)
SELECT d.service_id, d.domain, true, 'http-01', false
FROM service_domains d
JOIN services s ON s.id = d.service_id AND s.deleted_at IS NULL
WHERE NOT d.disabled AND d.protocol = 'https'
ON CONFLICT (service_id, domain) DO NOTHING;

-- +goose Down
-- Service intents have no representation in the previous schema.
DELETE FROM domain_certificates WHERE service_id IS NOT NULL;
DROP INDEX IF EXISTS domain_certificates_service_idx;
ALTER TABLE domain_certificates DROP COLUMN IF EXISTS owner_kind;
ALTER TABLE domain_certificates ADD COLUMN owner_kind text GENERATED ALWAYS AS (
    CASE WHEN application_id IS NULL THEN 'control_plane' ELSE 'application' END
) STORED;
DROP INDEX IF EXISTS domain_certificates_panel_domain_unique;
CREATE UNIQUE INDEX domain_certificates_panel_domain_unique
    ON domain_certificates (domain)
    WHERE application_id IS NULL;
ALTER TABLE domain_certificates DROP CONSTRAINT IF EXISTS domain_certificates_service_domain_unique;
ALTER TABLE domain_certificates DROP CONSTRAINT IF EXISTS domain_certificates_owner_check;
ALTER TABLE domain_certificates DROP COLUMN IF EXISTS service_id;
