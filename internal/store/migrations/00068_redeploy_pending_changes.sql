-- +goose Up
-- A setting that only reaches a new container (domains and ports, environment
-- variables, secrets, storage) marks the application as needing a redeploy.
-- pending_changes holds the canonical category codes; the next successful
-- deploy clears it. detail on a notification carries the same codes so the
-- inbox can localize them without storing rendered text.
ALTER TABLE applications
    ADD COLUMN pending_changes text[] NOT NULL DEFAULT '{}';

ALTER TABLE notifications
    ADD COLUMN detail text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE notifications DROP COLUMN detail;
ALTER TABLE applications DROP COLUMN pending_changes;
