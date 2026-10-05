-- +goose Up
-- Projects and environments (Phase 13, PE-1). A project groups a team's
-- workloads; each project holds 1..n environments (production is created with
-- the project) that PE-2 attaches applications, services and databases to.
--
-- Names are unique per parent, case-insensitively: (team_id, lower(name)) for
-- projects and (project_id, lower(name)) for environments.
--
-- The TRUNCATE below is deliberate: the product has no production data yet
-- (plan decision 6), so the resource tables are wiped instead of backfilled.
-- PE-2 re-adds environment_id and the per-environment uniqueness on the empty
-- tables. This is irreversible by design.
CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX projects_team_name_lower_idx ON projects (team_id, lower(name));
CREATE INDEX projects_team_idx ON projects (team_id, created_at DESC, id DESC);

CREATE TABLE environments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX environments_project_name_lower_idx ON environments (project_id, lower(name));
CREATE INDEX environments_project_idx ON environments (project_id, created_at ASC, id ASC);

-- Deliberate wipe (see the header comment): no production data exists, so the
-- resource rows are truncated instead of migrated. CASCADE reaches the rows
-- that hang off them (deployments, env vars, secrets, backups, previews,
-- webhooks, deploy keys). It does NOT reach proxy_config_versions: stale
-- proxy history stays, and orphan containers on nodes are untouched either
-- way. Both are acceptable because there is no production data (decision 6).
TRUNCATE applications, databases, services CASCADE;

-- +goose Down
-- The TRUNCATE above cannot be undone; Down only drops the two tables it
-- created.
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS projects;
