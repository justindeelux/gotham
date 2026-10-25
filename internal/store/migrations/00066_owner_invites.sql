-- +goose Up
-- Owner invites (GOTHAM-66): an invite carries the address it was issued to
-- and the projects the future member may access. Only the SHA-256 hash of the
-- token is stored; the raw token is returned exactly once at creation.
-- accepted_at marks a consumed token, so a replay can never mint a second
-- account or grant set.
CREATE TABLE invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    invited_by uuid REFERENCES users(id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX invites_created_idx ON invites (created_at DESC, id DESC);

-- The grants an invite hands out on acceptance. Deleting a project drops the
-- grant from every pending invite with it.
CREATE TABLE invite_projects (
    invite_id uuid NOT NULL REFERENCES invites(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (invite_id, project_id)
);

-- +goose Down
DROP TABLE invite_projects;
DROP TABLE invites;
