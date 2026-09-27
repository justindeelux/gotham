-- +goose Up
-- Source providers (GitHub / GitLab / Gitea). One row per (user, provider
-- type, host): the provider's own OAuth application plus, once connected, the
-- account access/refresh tokens. Credential columns are written encrypted at
-- rest (AES-256-GCM) by the providers service; the plaintext never reaches the
-- database. An empty base_url selects the provider's public host.

CREATE TABLE providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    base_url text NOT NULL DEFAULT '',
    client_id text NOT NULL DEFAULT '',
    client_secret text NOT NULL DEFAULT '',
    redirect_url text NOT NULL DEFAULT '',
    access_token text NOT NULL DEFAULT '',
    refresh_token text NOT NULL DEFAULT '',
    token_expires_at timestamptz,
    scopes text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- A user may keep one connection per provider host (self-hosted Gitea/GitLab
-- instances share the provider name but differ by base_url).
CREATE UNIQUE INDEX providers_user_name_base_idx
    ON providers (user_id, name, base_url);

-- repos_cache mirrors the repository list last fetched from a provider so the
-- UI keeps a usable list when the provider is briefly unreachable.
CREATE TABLE repos_cache (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id uuid NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    name text NOT NULL,
    full_name text NOT NULL,
    private boolean NOT NULL DEFAULT false,
    default_branch text NOT NULL DEFAULT '',
    clone_url text NOT NULL DEFAULT '',
    ssh_url text NOT NULL DEFAULT '',
    html_url text NOT NULL DEFAULT '',
    cached_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_cache_provider_external_key UNIQUE (provider_id, external_id)
);

CREATE INDEX repos_cache_provider_idx ON repos_cache (provider_id);

-- +goose Down
DROP TABLE IF EXISTS repos_cache;
DROP TABLE IF EXISTS providers;
