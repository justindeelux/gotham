-- name: UpsertApplicationGitCredential :one
INSERT INTO application_git_credentials (application_id, username, ciphertext, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (application_id) DO UPDATE SET
    username = EXCLUDED.username,
    ciphertext = EXCLUDED.ciphertext,
    updated_at = now()
RETURNING *;

-- name: GetApplicationGitCredential :one
SELECT * FROM application_git_credentials
WHERE application_id = $1;

-- name: DeleteApplicationGitCredential :exec
DELETE FROM application_git_credentials WHERE application_id = $1;
