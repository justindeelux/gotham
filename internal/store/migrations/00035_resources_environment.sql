-- +goose Up
-- Attach workloads to environments and pin their node (Phase 13, PE-2).
-- Tables are empty after 00034's deliberate truncate, so the new NOT NULL
-- columns need no backfill. Deleting an environment or a server that still
-- holds resources is refused (RESTRICT); the services map the violation to
-- the contract's 409. Names move from per-creator to per-environment
-- uniqueness; the per-server domain uniqueness is unchanged.

ALTER TABLE applications
    ADD COLUMN environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT;
ALTER TABLE applications DROP CONSTRAINT applications_server_id_fkey;
ALTER TABLE applications ALTER COLUMN server_id SET NOT NULL;
ALTER TABLE applications
    ADD CONSTRAINT applications_server_id_fkey FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE RESTRICT;
DROP INDEX applications_user_name_idx;
CREATE UNIQUE INDEX applications_environment_name_idx ON applications (environment_id, name);

ALTER TABLE databases
    ADD COLUMN environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT;
ALTER TABLE databases DROP CONSTRAINT databases_server_id_fkey;
ALTER TABLE databases ALTER COLUMN server_id SET NOT NULL;
ALTER TABLE databases
    ADD CONSTRAINT databases_server_id_fkey FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE RESTRICT;
DROP INDEX databases_user_name_idx;
CREATE UNIQUE INDEX databases_environment_name_idx ON databases (environment_id, name)
    WHERE deleted_at IS NULL;

ALTER TABLE services
    ADD COLUMN environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE RESTRICT;
ALTER TABLE services DROP CONSTRAINT services_server_id_fkey;
ALTER TABLE services ALTER COLUMN server_id SET NOT NULL;
ALTER TABLE services
    ADD CONSTRAINT services_server_id_fkey FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE RESTRICT;
DROP INDEX services_user_name_idx;
CREATE UNIQUE INDEX services_environment_name_idx ON services (environment_id, name)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS services_environment_name_idx;
ALTER TABLE services DROP CONSTRAINT services_server_id_fkey;
ALTER TABLE services ALTER COLUMN server_id DROP NOT NULL;
ALTER TABLE services
    ADD CONSTRAINT services_server_id_fkey FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE SET NULL;
ALTER TABLE services DROP COLUMN IF EXISTS environment_id;
CREATE UNIQUE INDEX services_user_name_idx ON services (user_id, name)
    WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS databases_environment_name_idx;
ALTER TABLE databases DROP CONSTRAINT databases_server_id_fkey;
ALTER TABLE databases ALTER COLUMN server_id DROP NOT NULL;
ALTER TABLE databases
    ADD CONSTRAINT databases_server_id_fkey FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE SET NULL;
ALTER TABLE databases DROP COLUMN IF EXISTS environment_id;
CREATE UNIQUE INDEX databases_user_name_idx ON databases (user_id, name)
    WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS applications_environment_name_idx;
ALTER TABLE applications DROP CONSTRAINT applications_server_id_fkey;
ALTER TABLE applications ALTER COLUMN server_id DROP NOT NULL;
ALTER TABLE applications
    ADD CONSTRAINT applications_server_id_fkey FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE SET NULL;
ALTER TABLE applications DROP COLUMN IF EXISTS environment_id;
CREATE UNIQUE INDEX applications_user_name_idx ON applications (user_id, name);
