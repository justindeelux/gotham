-- +goose Up
-- Application Compose source (GS-8): compose_content holds the pasted
-- compose file text (empty for repo-backed and every other source type),
-- compose_file holds the in-repo compose file path for repo-backed compose
-- sources (empty for pasted ones), and compose_service names the compose
-- service the application's domain/port routing targets. Existing rows
-- default to empty and are unaffected.

ALTER TABLE applications ADD COLUMN compose_content text NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN compose_file text NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN compose_service text NOT NULL DEFAULT '';

-- Each compose deployment records the exact document it ran: the raw
-- uninterpolated compose text (secrets stay references like ${VAR}, never
-- values) and, for repo-backed sources, the resolved commit. A rollback
-- re-renders the target deployment's document with the current environment,
-- so a bad edit can be rolled back without resurrecting rotated secrets.
-- The document is never returned by the API.
ALTER TABLE deployments ADD COLUMN compose_document text NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN compose_commit text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN IF EXISTS compose_commit;
ALTER TABLE deployments DROP COLUMN IF EXISTS compose_document;
ALTER TABLE applications DROP COLUMN IF EXISTS compose_service;
ALTER TABLE applications DROP COLUMN IF EXISTS compose_file;
ALTER TABLE applications DROP COLUMN IF EXISTS compose_content;
