-- name: CreateApplicationWebhook :one
INSERT INTO application_webhooks (application_id, provider, repo, hook_id, secret, url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetApplicationWebhookByApp :one
SELECT * FROM application_webhooks
WHERE application_id = $1;

-- name: DeleteApplicationWebhook :one
DELETE FROM application_webhooks
WHERE application_id = $1
RETURNING *;

-- name: GetApplicationForUser :one
SELECT * FROM applications
WHERE id = $1 AND user_id = $2;

-- name: ListWebhookTargetsForRepo :many
SELECT w.application_id, w.hook_id, w.secret, w.url,
       a.provider, a.repo, a.branch, a.clone_url
FROM application_webhooks w
JOIN applications a ON a.id = w.application_id
WHERE w.provider = $1 AND lower(w.repo) = $2;

-- name: CreateWebhookEvent :one
INSERT INTO webhook_events (application_id, provider, event, delivery_id, ref, commit_sha)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateWebhookEventDeployment :exec
UPDATE webhook_events
SET deployment_id = $2
WHERE id = $1;

-- name: DeleteWebhookEvent :exec
DELETE FROM webhook_events
WHERE id = $1;
