-- +goose Up
-- Teams and roles (Phase 8, BE-8.2). A team groups resources; a user belongs to
-- many teams and a resource belongs to exactly one team. Roles are owner,
-- admin and read_only.
--
-- Personal teams: every user owns exactly one personal team and its ID is the
-- owner's user ID. That invariant is what makes "no X-Team-Id header means the
-- caller's personal team" resolvable without a lookup, and it is what the
-- backfill below uses to attribute every pre-teams row to its creator's
-- personal team. Application code must keep creating personal teams this way
-- (auth registration, see store.CreateUserWithPersonalTeam).
CREATE TABLE teams (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    is_personal boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id),
    CONSTRAINT team_members_role_check CHECK (role IN ('owner', 'admin', 'read_only'))
);

-- Lists the teams of one user (GET /v1/teams).
CREATE INDEX team_members_user_idx ON team_members (user_id);

-- Invites join a team by email. Only the SHA-256 hash of the token is stored;
-- the raw token is returned exactly once at creation. An invite can grant
-- admin or read_only, never ownership: ownership changes are an explicit owner
-- action on the member list. accepted_at marks a consumed token; a consumed or
-- expired invite can never be accepted again.
CREATE TABLE invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    email text NOT NULL,
    role text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    invited_by uuid REFERENCES users(id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT invites_role_check CHECK (role IN ('admin', 'read_only'))
);

CREATE INDEX invites_team_idx ON invites (team_id, created_at DESC, id DESC);

-- One personal team per existing user, with the user as owner. The team ID is
-- the user ID (see the table comment above), so the backfill needs no mapping.
INSERT INTO teams (id, name, is_personal)
SELECT u.id, u.email || '''s team', true FROM users u;

INSERT INTO team_members (team_id, user_id, role)
SELECT u.id, u.id, 'owner' FROM users u;

-- Existing resources move from user scoping to team scoping. team_id is added
-- nullable, backfilled to the creator's personal team (again team_id = user_id)
-- and only then made NOT NULL, so the migration is safe on a populated
-- database. user_id stays: it records the creator, which is also what the
-- per-creator name indexes still key on.
ALTER TABLE applications ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE databases ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE CASCADE;
ALTER TABLE services ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE CASCADE;

UPDATE applications SET team_id = user_id;
UPDATE databases SET team_id = user_id;
UPDATE services SET team_id = user_id;

ALTER TABLE applications ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE databases ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE services ALTER COLUMN team_id SET NOT NULL;

CREATE INDEX applications_team_idx ON applications (team_id, created_at DESC);
CREATE INDEX databases_team_idx ON databases (team_id, created_at DESC);
CREATE INDEX services_team_idx ON services (team_id, created_at DESC);

-- Servers carry a nullable team_id instead: the table has no owner column, so
-- legacy rows cannot be attributed to a user. A NULL team_id keeps the
-- pre-teams behavior (visible to every authenticated caller) and is documented
-- as a Phase 8 residual; new servers are stamped with the creator's active
-- team. ON DELETE SET NULL: deleting a team must not delete the nodes.
ALTER TABLE servers ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE SET NULL;
CREATE INDEX servers_team_idx ON servers (team_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS servers_team_idx;
ALTER TABLE servers DROP COLUMN IF EXISTS team_id;

DROP INDEX IF EXISTS services_team_idx;
DROP INDEX IF EXISTS databases_team_idx;
DROP INDEX IF EXISTS applications_team_idx;

ALTER TABLE services DROP COLUMN IF EXISTS team_id;
ALTER TABLE databases DROP COLUMN IF EXISTS team_id;
ALTER TABLE applications DROP COLUMN IF EXISTS team_id;

DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
