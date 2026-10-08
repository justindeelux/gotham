-- +goose Up
-- Application Dockerfile source (GS-7): dockerfile_content holds the pasted
-- Dockerfile text (empty for every other source type), build_args holds the
-- optional --build-arg pairs as a JSON object. Existing rows default to empty
-- and are unaffected.
ALTER TABLE applications ADD COLUMN dockerfile_content text NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN build_args jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE applications DROP COLUMN IF EXISTS build_args;
ALTER TABLE applications DROP COLUMN IF EXISTS dockerfile_content;
