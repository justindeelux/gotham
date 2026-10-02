-- +goose Up
-- Restore runs (FX-9a). A backup row only records dumps; before this table an
-- interrupted restore left no trace, so a control-plane restart could neither
-- report it nor recover the database it had paused. A restore row is written
-- before the job starts and finished when it ends, so the boot-time sweep can
-- fail the interrupted ones and clean up their resources.
--
--	restores.status: running → completed | failed
--
-- backup_id has no foreign key on purpose: restore history is an audit trail
-- and must survive deletion of the artifact it was taken from.
CREATE TABLE restores (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id uuid NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    backup_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'running',
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CONSTRAINT restores_status_check CHECK (status IN ('running', 'completed', 'failed'))
);

CREATE INDEX restores_database_idx ON restores (database_id, created_at DESC);
CREATE INDEX restores_running_idx ON restores (created_at) WHERE status = 'running';

-- +goose Down
DROP TABLE IF EXISTS restores;
