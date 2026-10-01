-- name: GetUserByEmail :one
SELECT id, email, created_at, password_hash, avatar, updated_at
FROM users
WHERE lower(email) = lower($1);

-- name: GetUserByID :one
SELECT id, email, created_at, password_hash, avatar, updated_at
FROM users
WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING id, email, created_at, password_hash, avatar, updated_at;

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
-- path.
INSERT INTO users (email, password_hash)
SELECT $1, $2
WHERE NOT EXISTS (SELECT 1 FROM users)
RETURNING id, email, created_at, password_hash, avatar, updated_at;
