-- +goose Up
-- Proxy configuration history (BE-6.1 A2): the phase rollback plan keeps the
-- previous generated Traefik configuration for one day, so a bad push can be
-- reverted without regenerating from possibly-broken desired state. Rows are
-- pruned by the control plane after 24 hours.
CREATE TABLE proxy_config_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    files jsonb NOT NULL,
    content_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX proxy_config_versions_server_idx
    ON proxy_config_versions (server_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS proxy_config_versions;
