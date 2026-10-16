-- +goose Up
-- NULL application ownership is reserved for the control-plane panel. Existing
-- application/domain uniqueness and certificate rows remain unchanged.
-- Idempotent for development migration replay; no destructive downgrade.
ALTER TABLE domain_certificates ALTER COLUMN application_id DROP NOT NULL;
ALTER TABLE domain_certificates ADD COLUMN IF NOT EXISTS owner_kind text GENERATED ALWAYS AS (
    CASE WHEN application_id IS NULL THEN 'control_plane' ELSE 'application' END
) STORED;
CREATE UNIQUE INDEX IF NOT EXISTS domain_certificates_panel_domain_unique
    ON domain_certificates (domain) WHERE application_id IS NULL;
