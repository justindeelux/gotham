-- name: CreateTeam :one
INSERT INTO teams (id, name, is_personal)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = $1;

-- name: GetTeamForUser :one
-- GetTeamForUser returns one team together with the caller's role, or no row
-- when the caller is not a member. It is the authorization read of every
-- team-scoped route.
SELECT t.id, t.name, t.is_personal, t.created_at, t.updated_at, m.role AS member_role
FROM teams t
JOIN team_members m ON m.team_id = t.id
WHERE t.id = $1 AND m.user_id = $2;

-- name: ListTeamsByUser :many
SELECT t.id, t.name, t.is_personal, t.created_at, t.updated_at, m.role AS member_role
FROM teams t
JOIN team_members m ON m.team_id = t.id
WHERE m.user_id = $1
ORDER BY t.is_personal ASC, t.created_at DESC, t.id DESC;

-- name: UpdateTeam :one
UPDATE teams
SET name = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteTeam :exec
DELETE FROM teams WHERE id = $1;

-- name: CreateTeamMember :one
INSERT INTO team_members (team_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetTeamMember :one
SELECT * FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: ListTeamMembers :many
SELECT m.user_id, m.role, m.created_at, u.email
FROM team_members m
JOIN users u ON u.id = m.user_id
WHERE m.team_id = $1
ORDER BY m.created_at ASC, m.user_id ASC;

-- name: UpdateTeamMemberRole :one
UPDATE team_members
SET role = $3
WHERE team_id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteTeamMember :exec
DELETE FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: CountTeamOwners :one
SELECT count(*) FROM team_members WHERE team_id = $1 AND role = 'owner';

-- name: CreateInvite :one
INSERT INTO invites (team_id, email, role, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListInvitesByTeam :many
SELECT * FROM invites
WHERE team_id = $1
ORDER BY created_at DESC, id DESC;

-- name: GetInviteByTokenHash :one
SELECT * FROM invites WHERE token_hash = $1;

-- name: AcceptInvite :one
-- AcceptInvite consumes a pending, unexpired invite. A replayed, expired or
-- unknown token matches no row, so the caller can never double-accept.
UPDATE invites
SET accepted_at = now()
WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now()
RETURNING *;

-- name: DeleteInvite :exec
DELETE FROM invites WHERE id = $1 AND team_id = $2;
