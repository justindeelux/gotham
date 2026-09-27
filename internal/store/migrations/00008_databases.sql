-- +goose Up
-- Managed databases (Phase 5, BE-5.1). A database is a container on a managed
-- node plus a named volume (storage_path, "gotham-db-{id}") that survives the
-- container. The engine column pins the image family, version the tag, and
-- status the small lifecycle the API exposes:
--
--	creating → running ⇄ stopped
--	    ↘ error      deleting (soft: row kept, volume kept 7 days)
--
-- Deleting a database stops and removes the container but never the volume:
-- deleted_at is set and every read filters it out, so the data survives the
-- grace window before a permanent removal (Phase 5 rollback note).
CREATE TABLE databases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_id uuid REFERENCES servers(id) ON DELETE SET NULL,
    name text NOT NULL,
    engine text NOT NULL,
    version text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'creating',
    container_id text NOT NULL DEFAULT '',
    public_port int NOT NULL DEFAULT 0,
    storage_path text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT databases_engine_check CHECK (engine IN (
        'postgres', 'mysql', 'mariadb', 'mongodb', 'redis'
    )),
    CONSTRAINT databases_status_check CHECK (status IN (
        'creating', 'running', 'stopped', 'error', 'deleting'
    )),
    CONSTRAINT databases_public_port_check CHECK (public_port >= 0 AND public_port <= 65535)
);

-- The partial index lets a soft-deleted name be reused while two live
-- databases of one user can never collide.
CREATE UNIQUE INDEX databases_user_name_idx ON databases (user_id, name)
    WHERE deleted_at IS NULL;
CREATE INDEX databases_server_idx ON databases (server_id);

-- database_secrets is the credential store of one database, mirroring the
-- application `secrets` table of 00006 (which is keyed by a non-null
-- application_id and owned by Phase 4, so it cannot carry database rows).
-- ciphertext is base64(nonce||ciphertext) sealed with AES-256-GCM through
-- providers.SealSecret; plaintext only exists in the payload handed to the
-- node agent, never in this table. Keys in use: username, password, database
-- and root_password (MySQL/MariaDB).
CREATE TABLE database_secrets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id uuid NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    key text NOT NULL,
    ciphertext text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT database_secrets_db_key_unique UNIQUE (database_id, key)
);

CREATE INDEX database_secrets_db_idx ON database_secrets (database_id, key);

-- +goose Down
DROP TABLE IF EXISTS database_secrets;
DROP TABLE IF EXISTS databases;
