-- +goose Up
-- Extend users with credential material and replace the case-sensitive email
-- uniqueness with a case-insensitive index so logins are case-insensitive.
ALTER TABLE users
    ADD COLUMN password_hash text,
    ADD COLUMN avatar text,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE users DROP CONSTRAINT users_email_key;
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- Refresh-token sessions. Only a SHA-256 hash of the opaque refresh token is
-- stored, so a database leak does not hand out usable credentials.
CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- +goose Down
DROP TABLE IF EXISTS sessions;

DROP INDEX IF EXISTS users_email_lower_idx;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE users
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS avatar,
    DROP COLUMN IF EXISTS password_hash;
