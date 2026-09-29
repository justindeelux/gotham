-- +goose Up
-- Preview deployments (Phase 8, BE-8.1). One row per (application, pull
-- request) pair: it links the PR that triggered a preview to the sibling
-- application the preview actually runs on.
--
-- The base application (application_id) is the one whose webhook watched the
-- PR; the sibling (preview_application_id) is a normal applications row cloned
-- from the base configuration (branch = the PR head branch, its own
-- base_domain under the base domain). Deploying, routing and deleting a
-- preview therefore reuse the Phase 4 orchestrator and the Phase 6 proxy sync
-- unchanged — nothing here invents a second deploy engine.
--
-- The unique (application_id, pr_number) pair keeps a re-delivered PR event
-- idempotent: the same PR refreshes its row instead of racing a second
-- sibling. A closed PR keeps its row with state 'deleted' as the audit trail;
-- deleted_at records when the teardown happened.
--
-- preview_application_id is ON DELETE SET NULL, not CASCADE: the normal
-- teardown deletes the sibling application, and a cascade would erase the
-- audit row it is supposed to leave behind (state 'deleted' + deleted_at).
-- The application_id cascade still removes the binding with its base
-- application (the webhooks teardown path removes the siblings first, see
-- webhooks.DeletePreviews).
CREATE TABLE preview_deploys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    provider text NOT NULL,
    repo text NOT NULL,
    pr_number int NOT NULL,
    branch text NOT NULL DEFAULT '',
    head_sha text NOT NULL DEFAULT '',
    preview_application_id uuid REFERENCES applications(id) ON DELETE SET NULL,
    host text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'deploying',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT preview_deploys_state_check CHECK (
        state IN ('active', 'deploying', 'failed', 'deleted')
    ),
    CONSTRAINT preview_deploys_pr_number_check CHECK (pr_number > 0)
);

-- Idempotent (application, PR): a redelivered event updates the one row.
CREATE UNIQUE INDEX preview_deploys_app_pr_idx
    ON preview_deploys (application_id, pr_number);

-- The team surface (and the future previews screen): previews of one team.
CREATE INDEX preview_deploys_team_idx ON preview_deploys (team_id, created_at DESC);

-- The orphan sweep: live previews ordered by their last activity.
CREATE INDEX preview_deploys_updated_idx
    ON preview_deploys (updated_at)
    WHERE state <> 'deleted';

-- +goose Down
DROP TABLE IF EXISTS preview_deploys;
