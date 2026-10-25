-- +goose Up
-- Remove the team level (GOTHAM-64). Resources are instance-level now: every
-- team_id column and its indexes go, projects become unique by name across the
-- instance, and request receipts are keyed by principal alone. Data loss is
-- accepted: the team model never ran in production, so there is no backfill,
-- de-duplication or compatibility shim.
DROP INDEX IF EXISTS applications_team_idx;
ALTER TABLE applications DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS databases_team_idx;
ALTER TABLE databases DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS services_team_idx;
ALTER TABLE services DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS servers_team_idx;
ALTER TABLE servers DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS projects_team_name_lower_idx;
DROP INDEX IF EXISTS projects_team_idx;
ALTER TABLE projects DROP COLUMN IF EXISTS team_id;
-- The old per-team uniqueness allowed the same name in different teams. Keep
-- the earliest project of each name so the instance-wide index can be built;
-- the dropped duplicates are acceptable data loss.
DELETE FROM projects
WHERE id IN (
    SELECT id FROM (
        SELECT id, row_number() OVER (PARTITION BY lower(name) ORDER BY created_at, id) AS rn
        FROM projects
    ) ranked
    WHERE ranked.rn > 1
);
CREATE UNIQUE INDEX projects_name_lower_idx ON projects (lower(name));

DROP INDEX IF EXISTS notification_channels_team_idx;
DROP INDEX IF EXISTS notification_channels_scope_idx;
ALTER TABLE notification_channels DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS preview_deploys_team_idx;
ALTER TABLE preview_deploys DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS private_keys_team_idx;
ALTER TABLE private_keys DROP COLUMN IF EXISTS team_id;

ALTER TABLE request_receipts DROP CONSTRAINT IF EXISTS request_receipts_pkey;
ALTER TABLE request_receipts DROP COLUMN IF EXISTS team_id;
ALTER TABLE request_receipts
    ADD PRIMARY KEY (principal_id, operation, target_id, request_key);

DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;

-- Forward-only: the dropped team data cannot be restored.
