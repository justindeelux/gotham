-- +goose Up
-- Shared variables (Phase 13, PE-3): project- and environment-level KEY=VALUE
-- pairs merged beneath application variables at deploy time
-- (project < environment < application). A row with environment_id NULL is
-- project-level; otherwise it belongs to one environment of the same project
-- (the service checks the environment's project, so a row can never straddle
-- two projects). Secret rows keep their value sealed with
-- providers.SealSecret in ciphertext — the same AES-256-GCM helper as
-- application secrets — while value stays empty and is never returned.
CREATE TABLE shared_variables (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment_id uuid REFERENCES environments(id) ON DELETE CASCADE,
    key text NOT NULL,
    value text NOT NULL DEFAULT '',
    ciphertext text NOT NULL DEFAULT '',
    secret boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One scope, one key: COALESCE maps the project level (NULL) onto the nil
-- UUID so project rows and environment rows share the same uniqueness rule.
CREATE UNIQUE INDEX shared_variables_scope_key_idx ON shared_variables (project_id, (COALESCE(environment_id, '00000000-0000-0000-0000-000000000000')), key);
CREATE INDEX shared_variables_environment_idx ON shared_variables (environment_id) WHERE environment_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS shared_variables;
