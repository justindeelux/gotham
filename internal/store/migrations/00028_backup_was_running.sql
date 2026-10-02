-- +goose Up
-- FX-9a fix round 1: a crash-recovery sweep must only restart a database the
-- backup job actually paused. was_running records the pause observation, so
-- the sweep never starts a database the user had stopped before the job.
ALTER TABLE backups ADD COLUMN was_running boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE backups DROP COLUMN was_running;
