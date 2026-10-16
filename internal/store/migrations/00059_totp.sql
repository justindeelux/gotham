-- +goose Up
-- TOTP two-factor authentication (JUS-196). Kept in its own table rather than
-- added to users so the existing user queries and their generated row structs
-- are untouched. A missing row means TOTP was never configured; a row with
-- enabled = false is a pending enrollment (the secret is stored but no session
-- is challenged yet).
CREATE TABLE user_totp (
    user_id        uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- base32 secret, sealed at rest with the shared credential-encryption
    -- facility (providers.SealSecret), never stored or logged in the clear.
    secret_enc     text        NOT NULL,
    enabled        boolean     NOT NULL DEFAULT false,
    -- Highest RFC 6238 step already accepted for this account. A code at or
    -- below it is refused, so a used window cannot be replayed.
    last_step      bigint,
    -- argon2id hashes of the one-time recovery codes (one entry per unused
    -- code). A code is consumed by removing its hash; regenerating replaces
    -- the whole array.
    recovery_codes text[]      NOT NULL DEFAULT '{}',
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_totp;
