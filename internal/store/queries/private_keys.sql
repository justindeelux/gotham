-- name: CreatePrivateKey :one
INSERT INTO private_keys (name, encrypted_key, team_id)
VALUES ($1, $2, $3)
RETURNING id, name, created_at;

-- name: GetPrivateKeyByID :one
SELECT * FROM private_keys WHERE id = $1;

-- name: TeamReferencesPrivateKey :one
-- Reports whether the given team already attaches the key to one of its nodes.
-- keyForTeam uses it so a legacy key (team_id NULL) stays usable by the teams
-- that already reference it, and by no other team-scoped caller.
SELECT EXISTS(
    SELECT 1 FROM servers
    WHERE ssh_key_id = sqlc.arg(private_key_id) AND team_id = sqlc.arg(team_id)
);

-- name: ListPrivateKeysByTeam :many
-- The active team's keys plus every legacy key (team_id NULL), newest first,
-- mirroring ListServersByTeam. Listing a legacy key does not make it
-- attachable: keyForTeam still gates the attach.
SELECT id, name, created_at
FROM private_keys
WHERE team_id = $1 OR team_id IS NULL
ORDER BY created_at DESC, id DESC;

-- name: DeletePrivateKey :exec
DELETE FROM private_keys WHERE id = $1;
