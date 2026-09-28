-- +goose Up
-- Compose services (Phase 7, BE-7.1). A service is one docker-compose project
-- running on a managed node: compose_yaml is the user-supplied document and
-- env the substitution input the control plane renders it with before every
-- deploy. The rendered document is snapshotted per deploy in service_deploys
-- (00018), which is what makes a rollback a redeploy of the previous version.
--
-- Deleting a service soft-deletes the row (deleted_at) and never removes the
-- project's named volumes: a service row is versioned state, the volumes are
-- the data.
CREATE TABLE services (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_id uuid REFERENCES servers(id) ON DELETE SET NULL,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'creating',
    compose_yaml text NOT NULL,
    env jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT services_status_check CHECK (status IN (
        'creating', 'running', 'stopped', 'error', 'deleting'
    ))
);

-- The partial index lets a soft-deleted name be reused while two live services
-- of one user can never collide.
CREATE UNIQUE INDEX services_user_name_idx ON services (user_id, name)
    WHERE deleted_at IS NULL;
CREATE INDEX services_server_idx ON services (server_id);

-- +goose Down
DROP TABLE IF EXISTS services;
