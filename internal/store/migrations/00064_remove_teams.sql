-- +goose Up
-- Remove the team level (GOTHAM-64). Resources are instance-level now: every
-- team_id column and its indexes go, projects become unique by name across the
-- instance, and request receipts are keyed by principal alone. Data loss is
-- accepted: the team model never ran in production, so there is no backfill or
-- compatibility shim.
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
-- The old per-team uniqueness allowed the same name in different teams. Rename
-- every duplicate after the earliest one (by created_at) instead of deleting it:
-- projects own environments, and environments are referenced by applications,
-- services and databases with ON DELETE RESTRICT, so deleting a duplicate would
-- abort the migration. The ID suffix keeps each new name unique.
UPDATE projects AS p
SET name = p.name || ' (' || p.id::text || ')'
FROM (
    SELECT id FROM (
        SELECT id, row_number() OVER (PARTITION BY lower(name) ORDER BY created_at, id) AS rn
        FROM projects
    ) ranked
    WHERE ranked.rn > 1
) duplicate
WHERE p.id = duplicate.id;
CREATE UNIQUE INDEX IF NOT EXISTS projects_name_lower_idx ON projects (lower(name));

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

-- +goose Down
-- Irreversible: the team rows and the columns' data are gone. Fail loudly
-- instead of recording a rollback that leaves the schema changed.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 00064 removes the team model and cannot be rolled back; reset the database or restore a backup';
END $$;
-- +goose StatementEnd
