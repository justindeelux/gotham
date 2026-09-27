-- +goose Up
-- Applications deploy from Git (Phase 4, BE-4.3). An application is the parent
-- resource of every deployment: it pins the repository, branch, build pack and
-- the runtime shape (server, port, domains). Environment variables, secrets and
-- persistent storage hang off the application, not off a single deployment, so
-- every redeploy and rollback sees the same configuration.
CREATE TABLE applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_id uuid REFERENCES servers(id) ON DELETE SET NULL,
    name text NOT NULL,
    provider text NOT NULL DEFAULT '',
    repo text NOT NULL DEFAULT '',
    clone_url text NOT NULL DEFAULT '',
    branch text NOT NULL DEFAULT 'main',
    build_pack text NOT NULL DEFAULT 'auto',
    base_domain text NOT NULL DEFAULT '',
    port int NOT NULL DEFAULT 0,
    host_port int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT applications_port_check CHECK (port >= 0 AND port <= 65535),
    CONSTRAINT applications_host_port_check CHECK (host_port >= 0 AND host_port <= 65535)
);

CREATE UNIQUE INDEX applications_user_name_idx ON applications (user_id, name);
CREATE INDEX applications_server_idx ON applications (server_id);

-- deployments records one row per deploy attempt (and per rollback), moving
-- through the state machine queued -> cloning -> building -> pushing ->
-- starting -> running | failed. The image reference built for a deployment is
-- kept on the row forever: that is what rollback redeploys. The CHECK
-- constraints keep the state machine vocabulary honest at the database level,
-- and the partial unique index guarantees at most one in-flight deployment per
-- application (queued..starting are "active"; running and failed are not).
CREATE TABLE deployments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    kind text NOT NULL DEFAULT 'deploy',
    state text NOT NULL DEFAULT 'queued',
    image_tag text NOT NULL DEFAULT '',
    registry_image text NOT NULL DEFAULT '',
    digest text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    attempt int NOT NULL DEFAULT 0,
    container_id text NOT NULL DEFAULT '',
    rollback_from uuid,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT deployments_kind_check CHECK (kind IN ('deploy', 'rollback')),
    CONSTRAINT deployments_state_check CHECK (state IN (
        'queued', 'cloning', 'building', 'pushing', 'starting', 'running', 'failed'
    ))
);

CREATE INDEX deployments_app_idx ON deployments (application_id, created_at DESC);

CREATE UNIQUE INDEX deployments_active_app_idx ON deployments (application_id)
    WHERE state NOT IN ('running', 'failed');

-- env_vars are plain KEY=VALUE settings sent to the container (and usable as
-- build-time configuration). They are not confidential: secrets live below.
CREATE TABLE env_vars (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    key text NOT NULL,
    value text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT env_vars_app_key_unique UNIQUE (application_id, key)
);

-- secrets holds confidential KEY=VALUE settings. The value column carries
-- base64(nonce||ciphertext) sealed with AES-256-GCM (providers.SealSecret);
-- plaintext never reaches the database and is materialised only in the payload
-- sent to the node agent.
CREATE TABLE secrets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    key text NOT NULL,
    ciphertext text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT secrets_app_key_unique UNIQUE (application_id, key)
);

-- storages is the volume map of an application: a named persistent directory
-- on the node (host_path) mounted into the container (container_path).
CREATE TABLE storages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    name text NOT NULL,
    host_path text NOT NULL,
    container_path text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT storages_app_name_unique UNIQUE (application_id, name)
);

-- +goose Down
DROP TABLE IF EXISTS storages;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS env_vars;
DROP TABLE IF EXISTS deployments;
DROP TABLE IF EXISTS applications;
