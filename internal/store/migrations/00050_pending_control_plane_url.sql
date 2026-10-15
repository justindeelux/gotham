-- +goose Up
ALTER TABLE instance_settings ADD COLUMN pending_control_plane_url jsonb;

-- +goose Down
-- Forward-only: retain pending rollback state.
SELECT 1;
