-- +goose Up
-- Resource-agnostic backup core (GOTHAM-126). A backup, schedule or restore
-- now belongs to exactly one resource: a database, an application or a
-- service. The columns stay real foreign keys with ON DELETE CASCADE instead
-- of a polymorphic resource_id, so a deleted resource cannot leave orphaned
-- rows and every reference is validated by the database.
--
-- Existing rows keep their database_id; nothing is rewritten except the
-- destination of runs and schedules that never chose one, which now point at
-- the owner's built-in Local disk provider so the UI can name it.
ALTER TABLE backups
    ALTER COLUMN database_id DROP NOT NULL,
    ADD COLUMN application_id uuid REFERENCES applications(id) ON DELETE CASCADE,
    ADD COLUMN service_id uuid REFERENCES services(id) ON DELETE CASCADE,
    ADD CONSTRAINT backups_one_resource_check
        CHECK (num_nonnulls(application_id, service_id, database_id) = 1);

ALTER TABLE backup_schedules
    ALTER COLUMN database_id DROP NOT NULL,
    ADD COLUMN application_id uuid REFERENCES applications(id) ON DELETE CASCADE,
    ADD COLUMN service_id uuid REFERENCES services(id) ON DELETE CASCADE,
    ADD CONSTRAINT backup_schedules_one_resource_check
        CHECK (num_nonnulls(application_id, service_id, database_id) = 1);

ALTER TABLE restores
    ALTER COLUMN database_id DROP NOT NULL,
    ADD COLUMN application_id uuid REFERENCES applications(id) ON DELETE CASCADE,
    ADD COLUMN service_id uuid REFERENCES services(id) ON DELETE CASCADE,
    ADD CONSTRAINT restores_one_resource_check
        CHECK (num_nonnulls(application_id, service_id, database_id) = 1);

CREATE INDEX backups_application_idx ON backups (application_id, created_at DESC);
CREATE INDEX backups_service_idx ON backups (service_id, created_at DESC);
CREATE INDEX backup_schedules_application_idx ON backup_schedules (application_id);
CREATE INDEX backup_schedules_service_idx ON backup_schedules (service_id);
CREATE INDEX restores_application_idx ON restores (application_id, created_at DESC);
CREATE INDEX restores_service_idx ON restores (service_id, created_at DESC);

-- Storage providers are an owner-level catalogue. A built-in Local disk
-- provider exists from the first start (created for new installs by the backup
-- service) and cannot be deleted, so a fresh install can back up without any
-- setup and an artifact never falls back to an unnamed destination.
ALTER TABLE backup_targets
    ADD COLUMN builtin boolean NOT NULL DEFAULT false;

-- One built-in provider per account, so a resource owned by a member is
-- labelled the same way as the owner's resources. Provider management stays
-- owner-only; the extra rows only exist to name a local artifact.
INSERT INTO backup_targets (user_id, name, kind, builtin)
SELECT id, 'Local disk', 'local', true
FROM users
ON CONFLICT (user_id, name) DO NOTHING;

UPDATE backups b
SET target_id = t.id
FROM databases d
    JOIN backup_targets t ON t.user_id = d.user_id AND t.builtin
WHERE b.database_id = d.id AND b.target_id IS NULL;

UPDATE backup_schedules s
SET target_id = t.id
FROM databases d
    JOIN backup_targets t ON t.user_id = d.user_id AND t.builtin
WHERE s.database_id = d.id AND s.target_id IS NULL;

-- +goose Down
UPDATE backups SET target_id = NULL
FROM backup_targets t
WHERE t.builtin AND backups.target_id = t.id;

UPDATE backup_schedules SET target_id = NULL
FROM backup_targets t
WHERE t.builtin AND backup_schedules.target_id = t.id;

DELETE FROM backup_targets WHERE builtin;

ALTER TABLE backup_targets DROP COLUMN IF EXISTS builtin;

DROP INDEX IF EXISTS restores_application_idx;
DROP INDEX IF EXISTS restores_service_idx;
DROP INDEX IF EXISTS backup_schedules_application_idx;
DROP INDEX IF EXISTS backup_schedules_service_idx;
DROP INDEX IF EXISTS backups_application_idx;
DROP INDEX IF EXISTS backups_service_idx;

ALTER TABLE restores
    DROP CONSTRAINT IF EXISTS restores_one_resource_check,
    DROP COLUMN IF EXISTS application_id,
    DROP COLUMN IF EXISTS service_id;

ALTER TABLE backup_schedules
    DROP CONSTRAINT IF EXISTS backup_schedules_one_resource_check,
    DROP COLUMN IF EXISTS application_id,
    DROP COLUMN IF EXISTS service_id;

ALTER TABLE backups
    DROP CONSTRAINT IF EXISTS backups_one_resource_check,
    DROP COLUMN IF EXISTS application_id,
    DROP COLUMN IF EXISTS service_id;

ALTER TABLE restores ALTER COLUMN database_id SET NOT NULL;
ALTER TABLE backup_schedules ALTER COLUMN database_id SET NOT NULL;
ALTER TABLE backups ALTER COLUMN database_id SET NOT NULL;
