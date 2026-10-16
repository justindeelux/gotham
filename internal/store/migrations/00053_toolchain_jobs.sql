-- +goose Up
-- Persisted tool install/uninstall queue. One row per requested (node, tool,
-- action). The control plane enqueues rows here and a worker claims them in
-- order, driving the agent's ToolService and streaming the log back into the
-- row. The table survives a control-plane restart: a job left "running" is
-- reconciled to "failed" on boot (see FailInterruptedToolchainJobs).
CREATE TABLE toolchain_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    tool_id text NOT NULL,
    action text NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    log text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    CONSTRAINT toolchain_jobs_action_check CHECK (action IN ('install', 'uninstall')),
    CONSTRAINT toolchain_jobs_status_check CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled'))
);

-- The claim scan: oldest queued first, globally, then per node.
CREATE INDEX toolchain_jobs_status_created_idx ON toolchain_jobs (status, created_at, id);
-- Job listing per node, newest first.
CREATE INDEX toolchain_jobs_server_created_idx ON toolchain_jobs (server_id, created_at DESC, id DESC);
-- De-dupe: at most one active (queued/running) job per node+tool+action, so a
-- double enqueue or two racing requests cannot pile the same install twice. A
-- conflicting insert is a no-op the caller reports as "skipped".
CREATE UNIQUE INDEX toolchain_jobs_active_unique
    ON toolchain_jobs (server_id, tool_id, action)
    WHERE status IN ('queued', 'running');

-- +goose Down
DROP TABLE IF EXISTS toolchain_jobs;
