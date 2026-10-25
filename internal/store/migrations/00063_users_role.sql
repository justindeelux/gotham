-- +goose Up
-- Two-role account model (GOTHAM-63): the owner operates the instance and
-- every other account is a member. At most one owner exists; on an existing
-- database the earliest account keeps that role. The old model never ran in
-- production, so a dev database can also be reset instead.
ALTER TABLE users
    ADD COLUMN role text NOT NULL DEFAULT 'member'
    CHECK (role IN ('owner', 'member'));

UPDATE users
SET role = 'owner'
WHERE id = (
    SELECT id FROM users ORDER BY created_at ASC, id ASC LIMIT 1
);

CREATE UNIQUE INDEX users_single_owner_idx ON users (role) WHERE role = 'owner';

ALTER TABLE users DROP COLUMN IF EXISTS is_platform_admin;

-- +goose Down
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_platform_admin boolean NOT NULL DEFAULT FALSE;

UPDATE users SET is_platform_admin = TRUE WHERE role = 'owner';

DROP INDEX IF EXISTS users_single_owner_idx;
ALTER TABLE users DROP COLUMN IF EXISTS role;
