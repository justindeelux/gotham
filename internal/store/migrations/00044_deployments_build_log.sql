-- +goose Up
-- Persisted build log (JUS-84): the streamed lines of a deployment, stored
-- when the run reaches a terminal state so the log survives the Redis stream.
-- Capped at 256 KiB keeping the tail (see maxBuildLogBytes in
-- internal/deploy/buildlog.go). Deleted with the deployment/application row
-- by the existing cascades.

ALTER TABLE deployments ADD COLUMN build_log text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN IF EXISTS build_log;
