-- name: GetUserTOTP :one
-- GetUserTOTP returns the account's TOTP row. pgx.ErrNoRows means two-factor
-- authentication was never configured (or was reset).
SELECT user_id, secret_enc, enabled, last_step, recovery_codes, failed_attempts, locked_until
FROM user_totp
WHERE user_id = $1;

-- name: UpsertUserTOTPSecret :exec
-- UpsertUserTOTPSecret installs a pending secret (enabled stays false) or
-- replaces a pending one on a re-enrollment. The service refuses to call it
-- while TOTP is already enabled, so an active secret is never silently
-- swapped; last_step and recovery_codes are only touched by enable.
INSERT INTO user_totp (user_id, secret_enc, enabled, last_step, recovery_codes)
VALUES (sqlc.arg(user_id), sqlc.arg(secret_enc), false, NULL, '{}')
ON CONFLICT (user_id) DO UPDATE
SET secret_enc = EXCLUDED.secret_enc, updated_at = now();

-- name: EnableUserTOTP :one
-- EnableUserTOTP turns a pending secret on and installs its recovery-code set
-- while the row is still disabled. Zero rows means the row was enabled (or
-- removed) concurrently, so the caller must not treat the pending secret as
-- live. last_step is set one below the confirmation code's step, so a login in
-- the same 30-second window is accepted (once) rather than refused.
UPDATE user_totp
SET enabled = true,
    last_step = sqlc.arg(last_step)::bigint,
    recovery_codes = sqlc.arg(recovery_codes),
    failed_attempts = 0,
    locked_until = NULL,
    updated_at = now()
WHERE user_id = sqlc.arg(user_id)
  AND enabled = false
RETURNING user_id;

-- name: RecordUserTOTPFailure :one
-- RecordUserTOTPFailure increments the account's consecutive second-factor
-- failures and sets the lock deadline once the threshold is reached, in one
-- statement so concurrent guesses cannot lose a count. The caller computes the
-- deadline from its own clock (so tests can drive it); a row still below the
-- threshold keeps any existing deadline.
UPDATE user_totp
SET failed_attempts = failed_attempts + 1,
    locked_until = CASE
        WHEN failed_attempts + 1 >= sqlc.arg(threshold)::integer THEN sqlc.arg(locked_until)::timestamptz
        ELSE locked_until
    END,
    updated_at = now()
WHERE user_id = sqlc.arg(user_id)
RETURNING failed_attempts, locked_until;

-- name: ClearUserTOTPFailures :exec
-- ClearUserTOTPFailures resets the counter and drops any lock after a correct
-- code (TOTP or recovery), so the threshold counts consecutive failures only.
UPDATE user_totp
SET failed_attempts = 0, locked_until = NULL, updated_at = now()
WHERE user_id = $1;

-- name: AdvanceUserTOTPStep :one
-- AdvanceUserTOTPStep records the step a valid code was accepted at, but only
-- when it is strictly newer than the stored one: this is the replay guard. Zero
-- rows means the step was already used (or is older), so the caller refuses the
-- code. The comparison and the write are one statement, so two concurrent uses
-- of the same code cannot both succeed.
UPDATE user_totp
SET last_step = sqlc.arg(step)::bigint, updated_at = now()
WHERE user_id = sqlc.arg(user_id)
  AND enabled = true
  AND (last_step IS NULL OR last_step < sqlc.arg(step)::bigint)
RETURNING user_id;

-- name: ConsumeRecoveryCode :one
-- ConsumeRecoveryCode removes one recovery-code hash from the account's set,
-- and only while it is still present: a second use of the same code matches no
-- row, so a code is single use. The caller verifies the candidate against the
-- stored hashes first.
UPDATE user_totp
SET recovery_codes = array_remove(recovery_codes, sqlc.arg(code_hash)::text),
    updated_at = now()
WHERE user_id = sqlc.arg(user_id)
  AND enabled = true
  AND sqlc.arg(code_hash)::text = ANY(recovery_codes)
RETURNING user_id;

-- name: ReplaceUserTOTPRecoveryCodes :one
-- ReplaceUserTOTPRecoveryCodes swaps in a fresh recovery-code set and discards
-- every previous code. It runs only while TOTP is enabled, so a disable racing
-- this write wins and no orphan set survives.
UPDATE user_totp
SET recovery_codes = sqlc.arg(recovery_codes), updated_at = now()
WHERE user_id = sqlc.arg(user_id)
  AND enabled = true
RETURNING user_id;

-- name: DeleteUserTOTP :exec
-- DeleteUserTOTP removes the account's TOTP configuration (disable, or the
-- admin password reset that clears a lost device).
DELETE FROM user_totp
WHERE user_id = $1;
