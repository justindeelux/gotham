-- +goose Up
-- One certificate intent per domain (JUS-89): an application serving several
-- domains certifies each of them independently (each domain gets its own
-- HTTP-01 order or DNS-01 configuration), while a wildcard intent keeps
-- covering the sibling hosts its SAN matches (resolved at generation time).
-- The previous UNIQUE(application_id) allowed a single intent per
-- application, which cannot express per-domain certification.
--
-- Existing rows are unaffected: every application holds at most one intent,
-- so the new composite key admits all of them unchanged.
ALTER TABLE domain_certificates
    DROP CONSTRAINT domain_certificates_application_unique;

ALTER TABLE domain_certificates
    ADD CONSTRAINT domain_certificates_app_domain_unique UNIQUE (application_id, domain);

-- +goose Down
-- Rolling back re-adds UNIQUE(application_id): it fails while any
-- application holds more than one intent. Delete the extra intents first
-- when a downgrade must proceed.
ALTER TABLE domain_certificates
    DROP CONSTRAINT domain_certificates_app_domain_unique;

ALTER TABLE domain_certificates
    ADD CONSTRAINT domain_certificates_application_unique UNIQUE (application_id);
