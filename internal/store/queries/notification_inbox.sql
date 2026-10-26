-- name: CreateNotification :exec
INSERT INTO notifications (id, event_key, resource_type, resource_id, project_id, name, host, error, detail, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ListNotificationRecipients :many
SELECT u.id FROM users u
WHERE u.role = 'owner'
   OR EXISTS (
       SELECT 1 FROM project_members m
       WHERE m.user_id = u.id
         AND m.project_id = sqlc.narg('project_id')
   );

-- name: PruneNotifications :execrows
-- Retention: keep only the newest $1 rows. Run in the same transaction as the
-- insert so the inbox stays bounded.
DELETE FROM notifications AS n
WHERE n.id NOT IN (
    SELECT keep.id FROM notifications AS keep
    ORDER BY keep.created_at DESC, keep.id DESC
    LIMIT $1
);

-- name: ListVisibleNotifications :many
-- The owner sees every row; a member sees a project row only while the grant
-- exists and only when the row is not older than the grant. Instance-wide rows
-- have project_id NULL and are owner-only.
SELECT n.* FROM notifications n
WHERE EXISTS (
    SELECT 1 FROM users u
    WHERE u.id = $1
      AND (
          u.role = 'owner'
          OR EXISTS (
              SELECT 1 FROM project_members m
              WHERE m.user_id = u.id
                AND m.project_id = n.project_id
                AND n.created_at >= m.created_at
          )
      )
)
ORDER BY n.created_at DESC, n.id DESC
LIMIT $2;

-- name: CountUnreadNotifications :one
-- Unread means visible and newer than the marker; the page limit does not
-- apply because the bell counts the whole inbox.
SELECT COUNT(*) FROM notifications n
JOIN users u ON u.id = $1
WHERE n.created_at > u.notifications_seen_at
  AND (
      u.role = 'owner'
      OR EXISTS (
          SELECT 1 FROM project_members m
          WHERE m.user_id = u.id
            AND m.project_id = n.project_id
            AND n.created_at >= m.created_at
      )
  );

-- name: MarkNotificationsSeen :exec
-- Never moves backwards and never into the future.
UPDATE users
SET notifications_seen_at = GREATEST(notifications_seen_at, LEAST($2::timestamptz, now()))
WHERE id = $1;
