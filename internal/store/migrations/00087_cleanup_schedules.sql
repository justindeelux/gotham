-- +goose Up
-- Cleanup schedules run unattended on the owner's chosen nodes. A schedule
-- keeps its server list as an array: a node deleted later is skipped at run
-- time and reported as not_found, so history outlives the node.
CREATE TABLE cleanup_schedules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    server_ids    uuid[] NOT NULL CHECK (cardinality(server_ids) BETWEEN 1 AND 50),
    categories    text[] NOT NULL CHECK (cardinality(categories) >= 1),
    cron          text NOT NULL,
    min_age_hours integer NOT NULL DEFAULT 24 CHECK (min_age_hours BETWEEN 1 AND 8760),
    enabled       boolean NOT NULL DEFAULT true,
    next_run_at   timestamptz NOT NULL,
    last_run_at   timestamptz,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX cleanup_schedules_due_idx ON cleanup_schedules (next_run_at) WHERE enabled;

-- A run now links to the schedule that produced it. status/reason carry a
-- skipped node (offline, busy, unsupported) that freed nothing.
ALTER TABLE cleanup_runs ADD COLUMN schedule_id uuid REFERENCES cleanup_schedules (id) ON DELETE SET NULL;
ALTER TABLE cleanup_runs ADD COLUMN status text NOT NULL DEFAULT 'ok';
ALTER TABLE cleanup_runs ADD COLUMN reason text NOT NULL DEFAULT '';
CREATE INDEX cleanup_runs_schedule_idx ON cleanup_runs (schedule_id);

-- An exclusion protects an item on every trigger. A NULL server_id means every
-- node; match is the item name exactly as Scan reports it (or its id).
CREATE TABLE cleanup_exclusions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id  uuid REFERENCES servers (id) ON DELETE CASCADE,
    category   text NOT NULL,
    match      text NOT NULL,
    note       text NOT NULL DEFAULT '',
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX cleanup_exclusions_unique_idx
    ON cleanup_exclusions (COALESCE(server_id, '00000000-0000-0000-0000-000000000000'::uuid), category, match);
CREATE INDEX cleanup_exclusions_server_idx ON cleanup_exclusions (server_id);

-- +goose Down
DROP TABLE cleanup_exclusions;
DROP INDEX cleanup_runs_schedule_idx;
ALTER TABLE cleanup_runs DROP COLUMN reason, DROP COLUMN status, DROP COLUMN schedule_id;
DROP TABLE cleanup_schedules;
