-- +goose Up
-- Deployment commit metadata (JUS-82): the git commit a deployment cloned,
-- so the UI can show it in the Deployment section. Recorded after every
-- successful clone (empty for sources that never clone, and copied from the
-- target on rollback). The author name is stored without the email address.

ALTER TABLE deployments ADD COLUMN commit_sha text NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN commit_message text NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN commit_author text NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN committed_at text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN IF EXISTS committed_at;
ALTER TABLE deployments DROP COLUMN IF EXISTS commit_author;
ALTER TABLE deployments DROP COLUMN IF EXISTS commit_message;
ALTER TABLE deployments DROP COLUMN IF EXISTS commit_sha;
