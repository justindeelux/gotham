package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TOTP persistence (JUS-196). A missing row means two-factor authentication was
// never configured; a disabled row is a pending enrollment. The boolean results
// report whether the guarded statement matched a row: false means the account's
// TOTP state moved concurrently (already enabled, code step already used, or
// code already consumed), so the caller must fail closed.

// GetUserTOTP returns the account's TOTP row (pgx.ErrNoRows when unconfigured).
func (s *Store) GetUserTOTP(ctx context.Context, userID pgtype.UUID) (sqlc.GetUserTOTPRow, error) {
	return s.queries.GetUserTOTP(ctx, userID)
}

// UpsertUserTOTPSecret stores a pending (not yet enabled) secret.
func (s *Store) UpsertUserTOTPSecret(ctx context.Context, userID pgtype.UUID, secretEnc string) error {
	return s.queries.UpsertUserTOTPSecret(ctx, sqlc.UpsertUserTOTPSecretParams{
		UserID:    userID,
		SecretEnc: secretEnc,
	})
}

// EnableUserTOTP flips a pending secret on with its recovery-code hashes and
// the enrollment step, reporting false when the row was already enabled.
func (s *Store) EnableUserTOTP(ctx context.Context, userID pgtype.UUID, lastStep int64, recoveryCodes []string) (bool, error) {
	_, err := s.queries.EnableUserTOTP(ctx, sqlc.EnableUserTOTPParams{
		UserID:        userID,
		LastStep:      lastStep,
		RecoveryCodes: recoveryCodes,
	})
	return matched(err)
}

// AdvanceUserTOTPStep records an accepted code's step, reporting false when it
// was already used or is older (the replay guard).
func (s *Store) AdvanceUserTOTPStep(ctx context.Context, userID pgtype.UUID, step int64) (bool, error) {
	_, err := s.queries.AdvanceUserTOTPStep(ctx, sqlc.AdvanceUserTOTPStepParams{
		UserID: userID,
		Step:   step,
	})
	return matched(err)
}

// ConsumeRecoveryCode removes one recovery-code hash, reporting false when it
// was already consumed.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID pgtype.UUID, codeHash string) (bool, error) {
	_, err := s.queries.ConsumeRecoveryCode(ctx, sqlc.ConsumeRecoveryCodeParams{
		UserID:   userID,
		CodeHash: codeHash,
	})
	return matched(err)
}

// ReplaceUserTOTPRecoveryCodes swaps in a fresh recovery-code set, reporting
// false when TOTP is no longer enabled.
func (s *Store) ReplaceUserTOTPRecoveryCodes(ctx context.Context, userID pgtype.UUID, recoveryCodes []string) (bool, error) {
	_, err := s.queries.ReplaceUserTOTPRecoveryCodes(ctx, sqlc.ReplaceUserTOTPRecoveryCodesParams{
		UserID:        userID,
		RecoveryCodes: recoveryCodes,
	})
	return matched(err)
}

// RecordUserTOTPFailure increments the account's consecutive second-factor
// failures and arms the lockout until lockedUntil once threshold is reached, in
// one statement so two concurrent guesses cannot lose a count. A row still
// below the threshold keeps any existing deadline.
func (s *Store) RecordUserTOTPFailure(ctx context.Context, userID pgtype.UUID, threshold int, lockedUntil time.Time) (sqlc.RecordUserTOTPFailureRow, error) {
	return s.queries.RecordUserTOTPFailure(ctx, sqlc.RecordUserTOTPFailureParams{
		UserID:      userID,
		Threshold:   int32(threshold),
		LockedUntil: pgtype.Timestamptz{Time: lockedUntil, Valid: true},
	})
}

// ClearUserTOTPFailures resets the consecutive-failure counter and drops any
// lock after a correct code (TOTP or recovery).
func (s *Store) ClearUserTOTPFailures(ctx context.Context, userID pgtype.UUID) error {
	return s.queries.ClearUserTOTPFailures(ctx, userID)
}

// DeleteUserTOTP removes the account's TOTP configuration. It is idempotent.
func (s *Store) DeleteUserTOTP(ctx context.Context, userID pgtype.UUID) error {
	return s.queries.DeleteUserTOTP(ctx, userID)
}

// matched maps a guarded-statement result to its boolean: a missing row is
// reported as (false, nil), any other error passes through.
func matched(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
