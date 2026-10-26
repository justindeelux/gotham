-- +goose Up
-- In-app notification inbox (GOTHAM-73): structured facts only, no rendered
-- text and no per-item read flag. project_id NULL marks an instance-wide row,
-- which only the owner sees. notification rows survive channel removal because
-- nothing here references notification_channels.
CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key text NOT NULL,
    resource_type text,
    resource_id uuid,
    project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
    name text NOT NULL,
    host text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_created_idx ON notifications (created_at DESC, id DESC);
CREATE INDEX notifications_project_idx ON notifications (project_id, created_at DESC);

ALTER TABLE users ADD COLUMN notifications_seen_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE users DROP COLUMN notifications_seen_at;
DROP TABLE notifications;
