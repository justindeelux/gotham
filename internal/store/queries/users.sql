-- name: GetUserByEmail :one
SELECT id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin
FROM users
WHERE lower(email) = lower($1);

-- name: GetUserByID :one
SELECT id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin
FROM users
WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: UpdateUserPasswordHash :exec
-- UpdateUserPasswordHash replaces the password hash of the account with the
-- given (normalized, lowercase) email; callers resolve the exact address via
-- GetUserByEmail first.
UPDATE users
SET password_hash = $2, updated_at = now()
WHERE lower(email) = lower($1);

-- name: CreateFirstUser :one
-- CreateFirstUser inserts the bootstrap account only while the table is empty,
-- so two concurrent first registrations cannot both succeed (P-A2). Zero rows
-- means an account already exists and the caller must fall back to the invite
-- path. The bootstrap account is the platform admin (JUS-21); every other
-- account keeps the column default (false).
INSERT INTO users (email, password_hash, is_platform_admin)
SELECT $1, $2, TRUE
WHERE NOT EXISTS (SELECT 1 FROM users)
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin;

-- name: UpdateUserAvatar :one
-- UpdateUserAvatar replaces the account's stored OAuth avatar URL. The caller
-- (OAuth login) validates the URL against the shared allowlist first; a nil
-- avatar clears the stored value. It returns the updated row so the login can
-- mint its session from the fresh read.
UPDATE users
SET avatar = $2, updated_at = now()
WHERE id = $1
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin;

-- name: BumpCredentialVersion :exec
-- BumpCredentialVersion advances the account's credential version. It runs
-- inside the password-reset transaction: every session minted before the bump
-- carries the older version and Refresh refuses it.
UPDATE users
SET credential_version = credential_version + 1
WHERE id = $1;
