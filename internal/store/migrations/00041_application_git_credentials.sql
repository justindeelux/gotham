-- +goose Up
-- Private git HTTPS credentials (GS-4): one sealed token per application for
-- git_private sources that clone over HTTPS. The token is sealed with
-- AES-256-GCM (providers.SealSecret, the same shape as application secrets)
-- and opened only by the cloner and the connection test, which inject it
-- through an ephemeral GIT_ASKPASS helper — never in the clone URL, process
-- arguments or logs. The API never returns the token (only whether one is
-- set and the plaintext username). The row cascades with the application.
CREATE TABLE application_git_credentials (
    application_id uuid PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    username text NOT NULL DEFAULT '',
    ciphertext text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS application_git_credentials;
