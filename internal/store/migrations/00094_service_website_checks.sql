-- +goose Up
-- Website checks for managed service domains (GOTHAM-258).
--
-- The monitor tables were reachable only through application_domains: their
-- domain_id column carried a foreign key to that table. A service domain is a
-- row in service_domains with its own UUID, so the same tables can hold its
-- settings, state and probe history without a second set of tables.
--
-- domain_probe_results names its owner: exactly one of application_id and
-- service_id is set. Settings and state stay keyed by domain_id because a
-- domain UUID is unique across both tables.
ALTER TABLE domain_probe_results
    ALTER COLUMN application_id DROP NOT NULL,
    ADD COLUMN service_id uuid REFERENCES services(id) ON DELETE CASCADE,
    ADD CONSTRAINT domain_probe_results_owner_check
        CHECK ((application_id IS NULL) <> (service_id IS NULL));

-- A single domain_id column cannot reference both domain tables, so the
-- application foreign keys are replaced by the trigger below. Every delete
-- path (domain removal, application deletion, service deletion) fires it.
ALTER TABLE domain_monitor_settings
    DROP CONSTRAINT domain_monitor_settings_domain_id_fkey;
ALTER TABLE domain_monitor_state
    DROP CONSTRAINT domain_monitor_state_domain_id_fkey;
ALTER TABLE domain_probe_results
    DROP CONSTRAINT domain_probe_results_domain_id_fkey;

-- +goose StatementBegin
CREATE FUNCTION forget_domain_monitor_data() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM domain_monitor_settings WHERE domain_id = OLD.id;
    DELETE FROM domain_monitor_state WHERE domain_id = OLD.id;
    DELETE FROM domain_probe_results WHERE domain_id = OLD.id;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER application_domains_forget_monitor_data
    AFTER DELETE ON application_domains
    FOR EACH ROW EXECUTE FUNCTION forget_domain_monitor_data();

CREATE TRIGGER service_domains_forget_monitor_data
    AFTER DELETE ON service_domains
    FOR EACH ROW EXECUTE FUNCTION forget_domain_monitor_data();

-- +goose Down
DROP TRIGGER IF EXISTS service_domains_forget_monitor_data ON service_domains;
DROP TRIGGER IF EXISTS application_domains_forget_monitor_data ON application_domains;
DROP FUNCTION IF EXISTS forget_domain_monitor_data();

DELETE FROM domain_probe_results WHERE service_id IS NOT NULL;
DELETE FROM domain_monitor_settings
WHERE domain_id NOT IN (SELECT id FROM application_domains);
DELETE FROM domain_monitor_state
WHERE domain_id NOT IN (SELECT id FROM application_domains);

ALTER TABLE domain_probe_results
    DROP CONSTRAINT IF EXISTS domain_probe_results_owner_check,
    DROP COLUMN IF EXISTS service_id,
    ALTER COLUMN application_id SET NOT NULL;
ALTER TABLE domain_probe_results
    ADD CONSTRAINT domain_probe_results_domain_id_fkey
        FOREIGN KEY (domain_id) REFERENCES application_domains(id) ON DELETE CASCADE;
ALTER TABLE domain_monitor_state
    ADD CONSTRAINT domain_monitor_state_domain_id_fkey
        FOREIGN KEY (domain_id) REFERENCES application_domains(id) ON DELETE CASCADE;
ALTER TABLE domain_monitor_settings
    ADD CONSTRAINT domain_monitor_settings_domain_id_fkey
        FOREIGN KEY (domain_id) REFERENCES application_domains(id) ON DELETE CASCADE;
