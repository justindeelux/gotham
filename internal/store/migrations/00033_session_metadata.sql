-- +goose Up
-- Active sessions list (PF-2): device and network metadata per refresh
-- session, plus a last-use timestamp refreshed on every refresh-token
-- rotation (never per authenticated request). Existing rows backfill
-- last_used_at to created_at so nobody's session looks brand new.
ALTER TABLE sessions
    ADD COLUMN user_agent text NULL,
    ADD COLUMN ip inet NULL,
    ADD COLUMN last_used_at timestamptz NOT NULL DEFAULT now();

UPDATE sessions SET last_used_at = created_at;

-- +goose Down
ALTER TABLE sessions
    DROP COLUMN IF EXISTS last_used_at,
    DROP COLUMN IF EXISTS ip,
    DROP COLUMN IF EXISTS user_agent;
