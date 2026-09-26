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
