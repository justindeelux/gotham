-- name: CreateSession :one
INSERT INTO sessions (user_id, refresh_hash, expires_at, credential_version, user_agent, ip)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, user_id, refresh_hash, expires_at, revoked_at, created_at, credential_version, user_agent, ip, last_used_at;

-- name: GetSessionByRefreshHash :one
SELECT id, user_id, refresh_hash, expires_at, revoked_at, created_at, credential_version, user_agent, ip, last_used_at
FROM sessions
WHERE refresh_hash = $1;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE refresh_hash = $1
  AND revoked_at IS NULL;

-- name: DeleteSession :exec
-- DeleteSession removes the session with the given refresh hash only while it
-- is still live. Logout uses it instead of RevokeSession so a replayed
-- logged-out token reaches the reuse classifier as a deleted row, not a revoked
-- one: RevokeFamilyIfStolen returns false (nothing to attribute), so the user's
-- other live sessions survive and the refresh is a plain 401. An already-rotated
-- (revoked) row is left in place so its reuse-detection evidence survives.
DELETE FROM sessions
WHERE refresh_hash = $1
  AND revoked_at IS NULL;

-- name: DeleteUserSessions :exec
-- DeleteUserSessions removes every session of a user, used after a password
-- change so the old refresh chain dies with the credential.
DELETE FROM sessions
WHERE user_id = $1;

-- name: RevokeSessionIfLive :one
-- RevokeSessionIfLive revokes a session only while it is still live and
-- returns its id. Zero rows means the session was already revoked or deleted
-- (for example by a password reset racing this rotation), so the caller must
-- refuse to issue a replacement.
UPDATE sessions
SET revoked_at = now()
WHERE refresh_hash = $1
  AND revoked_at IS NULL
RETURNING id;

-- name: RevokeUserSessions :exec
-- RevokeUserSessions revokes every live session of a user. Refresh-token reuse
-- detection calls it when a replayed token reveals a possible theft: every
-- device is forced to re-authenticate.
UPDATE sessions
SET revoked_at = now()
WHERE user_id = $1
  AND revoked_at IS NULL;

-- name: RotateSession :one
-- RotateSession atomically revokes the presented live session and inserts its
-- replacement in one statement, so a failed insert or a cancelled request
-- cannot consume the user's only refresh token. Zero rows (pgx.ErrNoRows)
-- means the presented session was already revoked or deleted, so the caller
-- must refuse to issue a replacement.
WITH revoked AS (
    UPDATE sessions AS s
    SET revoked_at = now()
    WHERE s.refresh_hash = sqlc.arg(revoked_refresh_hash)
      AND s.revoked_at IS NULL
    RETURNING s.user_id
)
INSERT INTO sessions (user_id, refresh_hash, expires_at, credential_version, user_agent, ip)
SELECT r.user_id, sqlc.arg(new_refresh_hash), sqlc.arg(expires_at), sqlc.arg(credential_version), sqlc.arg(user_agent), sqlc.arg(ip)
FROM revoked AS r
RETURNING id, user_id, refresh_hash, expires_at, revoked_at, created_at, credential_version, user_agent, ip, last_used_at;

-- name: LockUserSessions :exec
-- LockUserSessions takes the transaction-scoped advisory lock that serializes
-- session changes for one user. Both the family-revocation path and the
-- rotation acquire it first, so a rotation cannot insert a replacement after a
-- reuse-detection snapshot has already read the user's live sessions.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(user_id)::text, 0));

-- name: DeleteStaleSessions :execrows
-- DeleteStaleSessions drops sessions that expired before the retention cutoff
-- and revoked sessions whose revocation predates the reuse window. A revoked
-- row is governed only by the revoked cutoff: its expiry must not shrink the
-- reuse-detection window, and revoked rows stay the full 30 days so a replayed
-- token can still revoke its family. The expired cutoff only buffers
-- plain-expired rows before deletion.
DELETE FROM sessions
WHERE (revoked_at IS NULL AND expires_at < sqlc.arg(expired_before))
   OR (revoked_at IS NOT NULL AND revoked_at < sqlc.arg(revoked_before));

-- name: ListSessionsByUser :many
-- ListSessionsByUser returns the caller's live sessions (not revoked, not
-- expired as of the given time), newest last use first, at most 50 rows. The
-- existing sessions_user_id_idx serves the per-user lookup: a user holds a
-- handful of rows, so no new index (measured: no sequential scan on realistic
-- per-user row counts; revisit if the sessions table grows hot).
SELECT id, user_agent, ip, created_at, last_used_at
FROM sessions
WHERE user_id = $1
  AND revoked_at IS NULL
  AND expires_at > $2
ORDER BY last_used_at DESC
LIMIT 50;

-- name: RevokeSessionByID :execrows
-- RevokeSessionByID ends one live session of a user. Zero rows means the id is
-- unknown, belongs to another user, or is already dead: the caller answers
-- 404 either way, so a probe cannot distinguish them.
UPDATE sessions
SET revoked_at = now()
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL;

-- name: RevokeOtherSessions :exec
-- RevokeOtherSessions ends every live session of a user except the given one
-- (the caller's current session, which keeps working).
UPDATE sessions
SET revoked_at = now()
WHERE user_id = $1
  AND id <> $2
  AND revoked_at IS NULL;
