-- +goose Up
CREATE TABLE project_members (
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    granted_by uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user_idx ON project_members (user_id, project_id);

-- Lists must apply grants before ordering and limiting, including resources
-- created by somebody else. Background resource lookups remain unrestricted.
-- +goose StatementBegin
CREATE FUNCTION can_access_environment(principal uuid, environment uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM users u WHERE u.id=principal AND (
            u.role='owner' OR EXISTS (
                SELECT 1 FROM project_members m
                JOIN environments e ON e.project_id=m.project_id
                WHERE m.user_id=u.id AND e.id=environment
            )
        )
    );
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION can_access_environment(uuid,uuid);
DROP TABLE project_members;
