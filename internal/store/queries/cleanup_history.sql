-- Cleanup history queries (GOTHAM-165). Each candidate is counted and removed
-- on the control plane itself: an aggregate item shows how much an owner would
-- reclaim, and every delete is batched so a large table never locks for long.
--
-- Deployment logs keep the newest three deployments of every application and
-- the log of the deployment currently running, so the two places an operator
-- actually reads (the latest attempts and the live container) never lose their
-- output.

-- name: BackedUpFileLocations :many
-- Every local artifact a backups row still owns. A file the cleanup scan does
-- not find here is an orphan.
SELECT DISTINCT location
FROM backups
WHERE location <> ''
ORDER BY location;

-- name: DeploymentHistoryStats :one
WITH newest AS (
    SELECT id
    FROM (
        SELECT id, row_number() OVER (
            PARTITION BY application_id ORDER BY created_at DESC, id DESC
        ) AS position
        FROM deployments
    ) ranked
    WHERE ranked.position <= 3
), current AS (
    SELECT id
    FROM (
        SELECT id, row_number() OVER (
            PARTITION BY application_id ORDER BY created_at DESC, id DESC
        ) AS position
        FROM deployments
        WHERE state = 'running'
    ) ranked
    WHERE ranked.position = 1
)
SELECT count(*)::bigint AS row_count,
    COALESCE(sum(pg_column_size(d.build_log)), 0)::bigint AS bytes
FROM deployments d
WHERE d.build_log <> ''
  AND d.created_at < $1
  AND d.state IN ('running', 'failed')
  AND d.id NOT IN (SELECT id FROM newest)
  AND d.id NOT IN (SELECT id FROM current);

-- name: ClearDeploymentHistory :execrows
WITH newest AS (
    SELECT id
    FROM (
        SELECT id, row_number() OVER (
            PARTITION BY application_id ORDER BY created_at DESC, id DESC
        ) AS position
        FROM deployments
    ) ranked
    WHERE ranked.position <= 3
), current AS (
    SELECT id
    FROM (
        SELECT id, row_number() OVER (
            PARTITION BY application_id ORDER BY created_at DESC, id DESC
        ) AS position
        FROM deployments
        WHERE state = 'running'
    ) ranked
    WHERE ranked.position = 1
), targets AS (
    SELECT d.id
    FROM deployments d
    WHERE d.build_log <> ''
      AND d.created_at < $1
      AND d.state IN ('running', 'failed')
      AND d.id NOT IN (SELECT id FROM newest)
      AND d.id NOT IN (SELECT id FROM current)
    LIMIT $2
)
UPDATE deployments d
SET build_log = '', updated_at = now()
FROM targets t
WHERE d.id = t.id;

-- name: ToolchainHistoryStats :one
-- A running job keeps its log regardless of age: it is still the live output
-- the operator is reading.
SELECT count(*)::bigint AS row_count,
    COALESCE(sum(pg_column_size(l.line)), 0)::bigint AS bytes
FROM toolchain_job_logs l
WHERE l.created_at < $1
  AND NOT EXISTS (
      SELECT 1 FROM toolchain_jobs j
      WHERE j.id = l.job_id AND j.status = 'running'
  );

-- name: DeleteToolchainHistory :execrows
DELETE FROM toolchain_job_logs
WHERE (job_id, seq) IN (
    SELECT l.job_id, l.seq
    FROM toolchain_job_logs l
    WHERE l.created_at < $1
      AND NOT EXISTS (
          SELECT 1 FROM toolchain_jobs j
          WHERE j.id = l.job_id AND j.status = 'running'
      )
    LIMIT $2
);

-- name: RequestReceiptHistoryStats :one
SELECT count(*)::bigint AS row_count,
    COALESCE(sum(pg_column_size(r)), 0)::bigint AS bytes
FROM request_receipts r
WHERE r.accepted_at < $1;

-- name: DeleteRequestReceiptHistory :execrows
DELETE FROM request_receipts
WHERE (principal_id, operation, target_id, request_key) IN (
    SELECT r.principal_id, r.operation, r.target_id, r.request_key
    FROM request_receipts r
    WHERE r.accepted_at < $1
    LIMIT $2
);

-- name: WebhookHistoryStats :one
SELECT count(*)::bigint AS row_count,
    COALESCE(sum(pg_column_size(w)), 0)::bigint AS bytes
FROM webhook_events w
WHERE w.received_at < $1;

-- name: DeleteWebhookHistory :execrows
DELETE FROM webhook_events
WHERE id IN (
    SELECT w.id
    FROM webhook_events w
    WHERE w.received_at < $1
    LIMIT $2
);
