-- +goose Up
-- Application source model (GS-2): source_type names how the application
-- fetches its code. Git-backed types share the deploy-key cloner; Dockerfile,
-- Compose and image sources land in GS-7..GS-9 and fail closed in the
-- orchestrator until then. Existing rows backfill from their provider
-- (github/gitlab keep the provider-backed flow, everything else is public
-- git); the cloner keys off the deploy key rather than this column, so no
-- existing deploy changes behaviour.
ALTER TABLE applications ADD COLUMN source_type text NOT NULL DEFAULT 'git_public';
ALTER TABLE applications
    ADD CONSTRAINT applications_source_type_check CHECK (source_type IN (
        'git_public', 'git_private', 'github_app', 'gitlab_app',
        'dockerfile', 'compose', 'image'
    ));
UPDATE applications SET source_type = 'github_app' WHERE provider = 'github';
UPDATE applications SET source_type = 'gitlab_app' WHERE provider = 'gitlab';
-- Gitea rows stay git_public: there is no gitea_app value in GS-2, and the
-- provider column is untouched, so the deploy-key and webhook flows that key
-- off provider keep working exactly as before. source_type only drives the
-- new wizard switch and the orchestrator fetch step, where every git-backed
-- type shares the deploy-key cloner.

-- +goose Down
ALTER TABLE applications DROP CONSTRAINT IF EXISTS applications_source_type_check;
ALTER TABLE applications DROP COLUMN IF EXISTS source_type;
