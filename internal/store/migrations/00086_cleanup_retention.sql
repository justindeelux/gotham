-- +goose Up
-- Cleanup retention (GOTHAM-165): the owner controls how long Gotham keeps its
-- own leftovers before they become cleanup candidates. One window covers every
-- history category; backup files get their own minimum age so a fresh partial
-- write is never offered.
ALTER TABLE instance_settings
    ADD COLUMN cleanup_history_retention_days integer NOT NULL DEFAULT 90
        CHECK (cleanup_history_retention_days BETWEEN 7 AND 3650),
    ADD COLUMN cleanup_backup_file_min_age_days integer NOT NULL DEFAULT 7
        CHECK (cleanup_backup_file_min_age_days BETWEEN 1 AND 365);

-- +goose Down
ALTER TABLE instance_settings
    DROP COLUMN IF EXISTS cleanup_backup_file_min_age_days,
    DROP COLUMN IF EXISTS cleanup_history_retention_days;
