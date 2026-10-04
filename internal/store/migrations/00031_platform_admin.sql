-- +goose Up
-- Platform admin flag (JUS-21): the account created on an empty instance
-- (first registration, OAuth bootstrap, or `gotham admin create` without
-- --force) is the platform admin. The column defaults to false, so every
-- pre-existing row stays non-admin: upgraded instances keep gating platform
-- operations on PLATFORM_ADMINS until an operator is promoted explicitly.
ALTER TABLE users ADD COLUMN is_platform_admin boolean NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS is_platform_admin;
