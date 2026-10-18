-- +goose Up
-- Per-file editor version history (JUS-224). Each row is the pre-save body of
-- one file on one node, keyed by the same (server, root, path) addressing the
-- file routes use. A server delete removes its history with it; retention
-- (count and bytes per file) is enforced transactionally on insert, so the
-- table stays bounded without a background sweep.
CREATE TABLE file_versions (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id  uuid        NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    root       text        NOT NULL,
    path       text        NOT NULL,
    etag       text        NOT NULL DEFAULT '',
    size       bigint      NOT NULL CHECK (size >= 0),
    content    bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- The list/prune/read key: every operation is scoped to one file.
CREATE INDEX file_versions_file_idx
    ON file_versions (server_id, root, path, created_at DESC, id DESC);

-- +goose Down
DROP TABLE file_versions;
