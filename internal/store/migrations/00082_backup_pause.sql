-- +goose Up
-- Volume backups default to stopping the resource while the archive is taken,
-- which is the same promise as a database dump. A schedule can opt out for a
-- resource whose writes tolerate an inconsistent snapshot. Databases ignore
-- the flag (they always dump with the engine stopped); existing schedules keep
-- the safe default.
ALTER TABLE backup_schedules
    ADD COLUMN pause_during_backup boolean NOT NULL DEFAULT true;

-- The choice is copied onto the run, so history still labels a run that was
-- taken live even after its schedule was edited or deleted.
ALTER TABLE backups
    ADD COLUMN pause_during_backup boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE backups DROP COLUMN IF EXISTS pause_during_backup;
ALTER TABLE backup_schedules DROP COLUMN IF EXISTS pause_during_backup;
