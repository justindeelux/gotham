-- +goose Up
-- Node servers managed by the control plane, plus the SSH private keys used to
-- reach them. Private keys are stored encrypted at rest (AES-256-GCM); the
-- plaintext PEM never touches the database.
CREATE TABLE private_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    encrypted_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE servers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    ip text NOT NULL,
    port int NOT NULL DEFAULT 22,
    ssh_user text NOT NULL,
    ssh_key_id uuid REFERENCES private_keys(id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'pending',
    node_id text UNIQUE,
    os text,
    docker_version text,
    arch text,
    total_mem bigint,
    total_disk bigint,
    cpu_usage double precision,
    mem_usage double precision,
    disk_usage double precision,
    container_count bigint,
    last_seen timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX servers_status_idx ON servers (status);

-- +goose Down
DROP TABLE IF EXISTS servers;
DROP TABLE IF EXISTS private_keys;
