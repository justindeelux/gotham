-- +goose Up
-- A project icon is either a built-in preset or an uploaded image. The icon
-- column carries the encoding; uploaded bytes live in project_icons so listing
-- projects never reads image data.
ALTER TABLE projects ADD COLUMN icon text NOT NULL DEFAULT '';
ALTER TABLE projects ADD CONSTRAINT projects_icon_check CHECK (
    icon = '' OR icon = 'upload' OR icon LIKE 'preset:%'
);
CREATE TABLE project_icons (
    project_id uuid PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    data bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE project_icons;
ALTER TABLE projects DROP CONSTRAINT projects_icon_check;
ALTER TABLE projects DROP COLUMN icon;
