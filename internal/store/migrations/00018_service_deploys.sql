-- +goose Up
-- One deploy attempt of a compose service (Phase 7, BE-7.1). compose_yaml is
-- the rendered document exactly as it was written to the node, so the
-- versioned deploy history is what a rollback redeploys. state tracks the
-- attempt: deploying while the node is being driven, running once the project
-- is up, failed when the agent rejected it (error carries the redacted
-- failure message, never a credential).
CREATE TABLE service_deploys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id uuid NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    state text NOT NULL DEFAULT 'deploying',
    compose_yaml text NOT NULL,
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CONSTRAINT service_deploys_state_check CHECK (state IN (
        'deploying', 'running', 'failed', 'stopped'
    ))
);

CREATE INDEX service_deploys_service_idx
    ON service_deploys (service_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS service_deploys;
