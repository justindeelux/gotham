-- +goose Up
-- GOTHAM-268: a monorepo app builds from a folder inside the clone instead of
-- its root. Empty means the repository root, so every existing app keeps its
-- current build context without an update.
ALTER TABLE applications
    ADD COLUMN working_dir text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE applications DROP COLUMN IF EXISTS working_dir;
