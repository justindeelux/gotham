-- +goose Up
-- Credential versioning (P-A5): a per-account monotonic counter that every
-- refresh session records, so a password reset ends every refresh chain even
-- when a login or refresh is in flight while the reset commits.
--
-- Both columns default to 1, so the upgrade backfills every existing session to
-- its account's current version (users are all 1 at this point): nobody is
-- logged out by the migration.
ALTER TABLE users ADD COLUMN credential_version integer NOT NULL DEFAULT 1;
ALTER TABLE sessions ADD COLUMN credential_version integer NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE sessions DROP COLUMN IF EXISTS credential_version;
ALTER TABLE users DROP COLUMN IF EXISTS credential_version;
