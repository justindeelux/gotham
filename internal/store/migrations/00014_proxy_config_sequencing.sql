-- +goose Up
-- Proxy configuration history sequencing (R2): the active snapshot is never
-- pruned, a pending row durably records the intent to replace it before the
-- node write, and a replaced predecessor is retained for 24 hours measured
-- from the replacement (superseded_at), not from its original push.
ALTER TABLE proxy_config_versions ADD COLUMN pending boolean NOT NULL DEFAULT false;
ALTER TABLE proxy_config_versions ADD COLUMN superseded_at timestamptz;

CREATE INDEX proxy_config_versions_pending_idx
    ON proxy_config_versions (server_id, pending);

-- +goose Down
DROP INDEX IF EXISTS proxy_config_versions_pending_idx;
ALTER TABLE proxy_config_versions DROP COLUMN IF EXISTS superseded_at;
ALTER TABLE proxy_config_versions DROP COLUMN IF EXISTS pending;
