-- +goose Up
-- pgcrypto provides gen_random_uuid(). PostgreSQL 13+ has it built in, but
-- creating the extension keeps the schema portable to older servers and is
-- harmless when it is already present.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS users;
