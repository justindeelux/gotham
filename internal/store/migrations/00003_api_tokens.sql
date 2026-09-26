-- +goose Up
-- Scoped API tokens for integrations (webhooks/CLI in Phase 4+). Only a
-- SHA-256 hash of the opaque token is stored, so a database leak does not hand
-- out usable credentials; the plaintext is shown exactly once at creation.
CREATE TABLE api_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    hash text NOT NULL UNIQUE,
    scopes text[] NOT NULL DEFAULT '{}',
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);

-- +goose Down
DROP TABLE IF EXISTS api_tokens;
