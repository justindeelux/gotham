-- +goose Up
-- GitHub App connections (GS-5, JUS-61): an app created through the manifest
-- flow plus its installations. Secrets (private key PEM, webhook secret) are
-- AES-GCM sealed with providers.SealSecret before they reach these columns;
-- the API never returns them.
CREATE TABLE github_apps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    app_id bigint NOT NULL,
    slug text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    base_url text NOT NULL DEFAULT 'https://github.com',
    api_base_url text NOT NULL DEFAULT 'https://api.github.com',
    client_id text NOT NULL DEFAULT '',
    webhook_secret_cipher text NOT NULL DEFAULT '',
    private_key_cipher text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, app_id)
);

CREATE TABLE github_installations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    github_app_id uuid NOT NULL REFERENCES github_apps (id) ON DELETE CASCADE,
    installation_id bigint NOT NULL,
    account text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (github_app_id, installation_id)
);

CREATE TABLE github_repo_cache (
    github_app_id uuid NOT NULL REFERENCES github_apps (id) ON DELETE CASCADE,
    installation_id bigint NOT NULL DEFAULT 0,
    external_id text NOT NULL DEFAULT '',
    name text NOT NULL DEFAULT '',
    full_name text NOT NULL DEFAULT '',
    private boolean NOT NULL DEFAULT false,
    default_branch text NOT NULL DEFAULT '',
    clone_url text NOT NULL DEFAULT '',
    ssh_url text NOT NULL DEFAULT '',
    html_url text NOT NULL DEFAULT '',
    cached_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (github_app_id, installation_id, external_id)
);

-- +goose Down
DROP TABLE IF EXISTS github_repo_cache;
DROP TABLE IF EXISTS github_installations;
DROP TABLE IF EXISTS github_apps;
