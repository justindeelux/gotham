-- +goose Up
-- OAuth sign-in providers are configured in Settings (GOTHAM-260). One row per
-- provider: client_secret is AES-GCM sealed with the instance secret key, and a
-- provider is only available while it is enabled with a client ID and secret.
CREATE TABLE oauth_providers (
    name          text        PRIMARY KEY,
    enabled       boolean     NOT NULL DEFAULT false,
    client_id     text        NOT NULL DEFAULT '',
    client_secret text        NOT NULL DEFAULT '',
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE oauth_providers;
