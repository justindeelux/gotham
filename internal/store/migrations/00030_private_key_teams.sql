-- +goose Up
-- Team scoping for SSH private keys (JUS-5 fix round 1, defect 4a). Keys were
-- created without a team, so any caller could attach any key to their node by
-- UUID. New keys are stamped with the caller's active team; pre-existing keys
-- keep team_id NULL and stay visible to every caller, which mirrors the legacy
-- shared-node rule for servers (team_isolation_integration_test.go).
ALTER TABLE private_keys ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE RESTRICT;
CREATE INDEX private_keys_team_idx ON private_keys (team_id);

-- +goose Down
DROP INDEX IF EXISTS private_keys_team_idx;
ALTER TABLE private_keys DROP COLUMN IF EXISTS team_id;
