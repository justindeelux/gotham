-- +goose Up
-- Retention and cleanup (GOTHAM-131).
--
-- keep_last is the per-schedule retention limit. New schedules default to 7 in
-- the API; existing schedules stay NULL ("keep everything") so an upgrade never
-- deletes a backup the owner did not agree to lose.
ALTER TABLE backup_schedules
    ADD COLUMN keep_last integer,
    ADD CONSTRAINT backup_schedules_keep_last_check
        CHECK (keep_last IS NULL OR keep_last BETWEEN 1 AND 365);

-- A run being removed first becomes cleanup_pending. The artifact is deleted
-- from its provider, then the row; a provider outage leaves the row pending so
-- the sweeper can retry instead of losing track of the artifact.
ALTER TABLE backups
    ADD COLUMN cleanup_pending boolean NOT NULL DEFAULT false,
    ADD COLUMN cleanup_error text NOT NULL DEFAULT '',
    ADD COLUMN cleanup_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN cleanup_next_attempt_at timestamptz,
    ADD COLUMN provider_removed boolean NOT NULL DEFAULT false;

CREATE INDEX backups_cleanup_pending_idx ON backups (cleanup_next_attempt_at)
    WHERE cleanup_pending;

-- Artifacts of a hard-deleted resource are queued here before the resource row
-- (and its backup rows) is removed. The queue has no foreign key to the
-- resource: it survives the cascade and is drained by the cleanup sweeper.
CREATE TABLE backup_artifact_cleanup (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id uuid REFERENCES backup_targets(id) ON DELETE SET NULL,
    location text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX backup_artifact_cleanup_due_idx
    ON backup_artifact_cleanup (next_attempt_at);

-- +goose StatementBegin
CREATE FUNCTION queue_backup_artifacts() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'applications' THEN
        INSERT INTO backup_artifact_cleanup (target_id, location)
        SELECT target_id, location FROM backups
        WHERE application_id = OLD.id AND location <> '';
    ELSIF TG_TABLE_NAME = 'services' THEN
        INSERT INTO backup_artifact_cleanup (target_id, location)
        SELECT target_id, location FROM backups
        WHERE service_id = OLD.id AND location <> '';
    ELSIF TG_TABLE_NAME = 'databases' THEN
        INSERT INTO backup_artifact_cleanup (target_id, location)
        SELECT target_id, location FROM backups
        WHERE database_id = OLD.id AND location <> '';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER applications_queue_backup_artifacts
    BEFORE DELETE ON applications
    FOR EACH ROW EXECUTE FUNCTION queue_backup_artifacts();

CREATE TRIGGER services_queue_backup_artifacts
    BEFORE DELETE ON services
    FOR EACH ROW EXECUTE FUNCTION queue_backup_artifacts();

CREATE TRIGGER databases_queue_backup_artifacts
    BEFORE DELETE ON databases
    FOR EACH ROW EXECUTE FUNCTION queue_backup_artifacts();

-- +goose Down
DROP TRIGGER IF EXISTS databases_queue_backup_artifacts ON databases;
DROP TRIGGER IF EXISTS services_queue_backup_artifacts ON services;
DROP TRIGGER IF EXISTS applications_queue_backup_artifacts ON applications;
DROP FUNCTION IF EXISTS queue_backup_artifacts();
DROP TABLE IF EXISTS backup_artifact_cleanup;

DROP INDEX IF EXISTS backups_cleanup_pending_idx;

ALTER TABLE backups
    DROP COLUMN IF EXISTS provider_removed,
    DROP COLUMN IF EXISTS cleanup_next_attempt_at,
    DROP COLUMN IF EXISTS cleanup_attempts,
    DROP COLUMN IF EXISTS cleanup_error,
    DROP COLUMN IF EXISTS cleanup_pending;

ALTER TABLE backup_schedules
    DROP CONSTRAINT IF EXISTS backup_schedules_keep_last_check,
    DROP COLUMN IF EXISTS keep_last;
