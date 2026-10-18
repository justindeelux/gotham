-- name: InsertFileVersion :one
-- InsertFileVersion records one pre-save body. The row is pruned by
-- SaveFileVersion in the same transaction, so a snapshot never leaves the
-- table above the configured retention.
INSERT INTO file_versions (server_id, root, path, etag, size, content)
VALUES (
    sqlc.arg(server_id),
    sqlc.arg(root),
    sqlc.arg(path),
    sqlc.arg(etag),
    sqlc.arg(size)::bigint,
    sqlc.arg(content)
)
RETURNING id, etag, size, created_at;

-- name: ListFileVersions :many
-- ListFileVersions returns one file's revisions newest first, without the
-- bodies (the list view never reads history content).
SELECT id, etag, size, created_at
FROM file_versions
WHERE server_id = $1 AND root = $2 AND path = $3
ORDER BY created_at DESC, id DESC;

-- name: GetFileVersion :one
-- GetFileVersion reads one revision's body. The file key is part of the
-- lookup, so a version id from another file or node is never returned.
SELECT id, etag, size, created_at, content
FROM file_versions
WHERE id = sqlc.arg(id)
  AND server_id = sqlc.arg(server_id)
  AND root = sqlc.arg(root)
  AND path = sqlc.arg(path);

-- name: DeleteFileVersion :execrows
-- DeleteFileVersion removes one revision of the addressed file.
DELETE FROM file_versions
WHERE id = sqlc.arg(id)
  AND server_id = sqlc.arg(server_id)
  AND root = sqlc.arg(root)
  AND path = sqlc.arg(path);

-- name: PruneFileVersionsByCount :exec
-- PruneFileVersionsByCount keeps only the newest max_versions rows of one file.
DELETE FROM file_versions AS f
WHERE f.server_id = sqlc.arg(server_id)
  AND f.root = sqlc.arg(root)
  AND f.path = sqlc.arg(path)
  AND f.id NOT IN (
    SELECT v.id FROM file_versions AS v
    WHERE v.server_id = sqlc.arg(server_id)
      AND v.root = sqlc.arg(root)
      AND v.path = sqlc.arg(path)
    ORDER BY v.created_at DESC, v.id DESC
    LIMIT sqlc.arg(max_versions)::bigint
  );

-- name: PruneFileVersionsBySize :exec
-- PruneFileVersionsBySize drops the oldest revisions until the retained
-- bodies fit max_bytes, newest first.
DELETE FROM file_versions AS f
WHERE f.server_id = sqlc.arg(server_id)
  AND f.root = sqlc.arg(root)
  AND f.path = sqlc.arg(path)
  AND f.id IN (
    SELECT ranked.id FROM (
      SELECT v.id,
             CAST(SUM(v.size) OVER (ORDER BY v.created_at DESC, v.id DESC) AS bigint) AS running_bytes
      FROM file_versions AS v
      WHERE v.server_id = sqlc.arg(server_id)
        AND v.root = sqlc.arg(root)
        AND v.path = sqlc.arg(path)
    ) ranked
    WHERE ranked.running_bytes > sqlc.arg(max_bytes)::bigint
  );
