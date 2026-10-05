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
    -- The environment reference is enforced by the composite foreign key
    -- below (it must name an environment of this row's own project), so the
    -- column itself carries no inline reference.
    environment_id uuid,
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

-- An environment-level row must belong to its own project: the composite
-- foreign key ties (environment_id, project_id) to the environment it names.
-- It needs a unique (id, project_id) on environments (id alone is already the
-- primary key, so the pair is trivially unique). Project-level rows carry a
-- NULL environment_id, which always satisfies a composite foreign key.
CREATE UNIQUE INDEX environments_id_project_idx ON environments (id, project_id);
ALTER TABLE shared_variables
    ADD CONSTRAINT shared_variables_environment_project_fkey
    FOREIGN KEY (environment_id, project_id) REFERENCES environments (id, project_id) ON DELETE CASCADE;

-- +goose Down
DROP TABLE IF EXISTS shared_variables;
DROP INDEX IF EXISTS environments_id_project_idx;
