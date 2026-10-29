-- +goose Up
-- The catalog template a service was created from, so the console can show the
-- stack's brand. Compose imports and rows created before this migration keep
-- NULL: the origin cannot be recovered reliably, so they fall back to the
-- compose mark.
ALTER TABLE services ADD COLUMN template_slug text;

-- +goose Down
ALTER TABLE services DROP COLUMN IF EXISTS template_slug;
