-- +goose Up
-- Team scoping for SSH private keys (JUS-5 fix round 1, defect 4a). Keys were
-- created without a team, so any caller could attach any key to their node by
-- UUID. New keys are stamped with the caller's active team.
ALTER TABLE private_keys ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE RESTRICT;
CREATE INDEX private_keys_team_idx ON private_keys (team_id);

-- Backfill (JUS-5 fix round 2): attribute each pre-existing key to its owning
-- team so legacy keys stop being attachable by arbitrary teams.
--   1. Deploy-key rows follow their application: the mapping is per-app, so
--      the app's team is the stronger ownership signal and wins when a key is
--      both a deploy key and a node key.
--   2. The remaining keys follow the servers that reference them, but only
--      when every referencing server agrees on one non-NULL team. A key
--      referenced by servers of more than one team, by a legacy (team_id NULL)
--      server, or by no server keeps team_id NULL.
-- A still-NULL key is usable only by callers without a team scope (the legacy
-- pre-teams path) and by team callers whose own team already references the
-- key (see ServerService.keyForTeam); it is never attachable by an unrelated
-- team that merely learns the UUID.
UPDATE private_keys pk
SET team_id = app.team_id
FROM application_deploy_keys dk
JOIN applications app ON app.id = dk.application_id
WHERE pk.id = dk.private_key_id
  AND pk.team_id IS NULL;

UPDATE private_keys pk
SET team_id = attributed.team_id
FROM (
    -- MIN over the text cast: uuid has no MIN aggregate, and the HAVING
    -- below guarantees a single distinct team, so any deterministic pick is
    -- that team.
    SELECT s.ssh_key_id AS key_id, MIN(s.team_id::text)::uuid AS team_id
    FROM servers s
    WHERE s.ssh_key_id IS NOT NULL
    GROUP BY s.ssh_key_id
    HAVING COUNT(*) = COUNT(s.team_id)
       AND COUNT(DISTINCT s.team_id) = 1
) AS attributed
WHERE pk.id = attributed.key_id
  AND pk.team_id IS NULL;

-- +goose Down
DROP INDEX IF EXISTS private_keys_team_idx;
ALTER TABLE private_keys DROP COLUMN IF EXISTS team_id;
