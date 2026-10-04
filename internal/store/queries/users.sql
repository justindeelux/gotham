-- name: GetUserByEmail :one
SELECT id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name
FROM users
WHERE lower(email) = lower($1);

-- name: GetUserByID :one
SELECT id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name
FROM users
WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name;

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
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name;

-- name: UpdateUserAvatar :one
-- UpdateUserAvatar replaces the account's stored OAuth avatar URL. The caller
-- (OAuth login) validates the URL against the shared allowlist first; a nil
-- avatar clears the stored value. It returns the updated row so the login can
-- mint its session from the fresh read.
UPDATE users
SET avatar = $2, updated_at = now()
WHERE id = $1
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name;

-- name: BumpCredentialVersion :exec
-- BumpCredentialVersion advances the account's credential version. It runs
-- inside the password-reset transaction: every session minted before the bump
-- carries the older version and Refresh refuses it.
UPDATE users
SET credential_version = credential_version + 1
WHERE id = $1;

-- name: UpdateUserDisplayName :one
-- UpdateUserDisplayName replaces the account's display name. A nil name clears
-- it. Bounds (1-64 characters) are enforced by the caller and the CHECK
-- constraint; it returns the updated row so the caller can render it.
UPDATE users
SET display_name = $2, updated_at = now()
WHERE id = $1
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name;

-- name: SetUserPasswordHash :one
-- SetUserPasswordHash replaces the account's password hash and bumps its
-- credential version in one statement. The match on the previous hash makes a
-- concurrent password change visible as zero rows (pgx.ErrNoRows): the
-- caller's current-password check ran against a stale credential. It runs
-- inside the caller's transaction (see Store.ChangeUserPassword), next to the
-- session delete, so the hash, the version and the session purge commit
-- together. It returns the updated row so the caller can mint its fresh
-- session from the post-bump version.
UPDATE users
SET password_hash = $2, updated_at = now(), credential_version = credential_version + 1
WHERE id = $1 AND password_hash IS NOT DISTINCT FROM $3
RETURNING id, email, created_at, password_hash, avatar, updated_at, credential_version, is_platform_admin, display_name;
