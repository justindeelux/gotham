-- +goose Up
-- Cleanup runs are the audit record for node cleanups and the history the
-- cleanup center shows. A run is written after the node answered, so finished_at
-- is always known; reclaimed_bytes is the sum of its item results.
CREATE TABLE cleanup_runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    actor_user_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    trigger         text NOT NULL,
    started_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz NOT NULL DEFAULT now(),
    reclaimed_bytes bigint NOT NULL DEFAULT 0,
    CONSTRAINT cleanup_runs_trigger_check CHECK (trigger IN ('manual', 'schedule', 'threshold'))
);

CREATE INDEX cleanup_runs_server_idx ON cleanup_runs (server_id, started_at DESC);

CREATE TABLE cleanup_run_items (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          uuid NOT NULL REFERENCES cleanup_runs (id) ON DELETE CASCADE,
    item_id         text NOT NULL,
    category        text NOT NULL,
    name            text NOT NULL,
    status          text NOT NULL,
    message         text NOT NULL DEFAULT '',
    reclaimed_bytes bigint NOT NULL DEFAULT 0
);

CREATE INDEX cleanup_run_items_run_idx ON cleanup_run_items (run_id);

-- +goose Down
DROP TABLE cleanup_run_items;
DROP TABLE cleanup_runs;
