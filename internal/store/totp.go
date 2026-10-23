package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetUserTOTP(ctx context.Context, userID pgtype.UUID) (sqlc.GetUserTOTPRow, error) {
	return s.queries.GetUserTOTP(ctx, userID)
}

func (s *Store) UpsertUserTOTPSecret(ctx context.Context, userID pgtype.UUID, secretEnc string) (bool, error) {
	_, err := s.queries.UpsertUserTOTPSecret(ctx, sqlc.UpsertUserTOTPSecretParams{
		UserID:    userID,
		SecretEnc: secretEnc,
	})
	return matched(err)
}

func (s *Store) EnableUserTOTP(ctx context.Context, userID pgtype.UUID, lastStep int64, recoveryCodes []string) (bool, error) {
	_, err := s.queries.EnableUserTOTP(ctx, sqlc.EnableUserTOTPParams{
		UserID:        userID,
		LastStep:      lastStep,
		RecoveryCodes: recoveryCodes,
	})
	return matched(err)
}

func (s *Store) AdvanceUserTOTPStep(ctx context.Context, userID pgtype.UUID, step int64) (bool, error) {
	_, err := s.queries.AdvanceUserTOTPStep(ctx, sqlc.AdvanceUserTOTPStepParams{
		UserID: userID,
		Step:   step,
	})
	return matched(err)
}

func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID pgtype.UUID, codeHash string) (bool, error) {
	_, err := s.queries.ConsumeRecoveryCode(ctx, sqlc.ConsumeRecoveryCodeParams{
		UserID:   userID,
		CodeHash: codeHash,
	})
	return matched(err)
}

func (s *Store) ReplaceUserTOTPRecoveryCodes(ctx context.Context, userID pgtype.UUID, recoveryCodes []string) (bool, error) {
	_, err := s.queries.ReplaceUserTOTPRecoveryCodes(ctx, sqlc.ReplaceUserTOTPRecoveryCodesParams{
		UserID:        userID,
		RecoveryCodes: recoveryCodes,
	})
	return matched(err)
}

func (s *Store) BeginUserTOTPAttempt(ctx context.Context, userID pgtype.UUID, threshold int, now, lockedUntil time.Time) (bool, error) {
	_, err := s.queries.BeginUserTOTPAttempt(ctx, sqlc.BeginUserTOTPAttemptParams{
		UserID:      userID,
		Threshold:   int32(threshold),
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
		LockedUntil: pgtype.Timestamptz{Time: lockedUntil, Valid: true},
	})
	return matched(err)
}

func (s *Store) ClearUserTOTPFailures(ctx context.Context, userID pgtype.UUID) error {
	return s.queries.ClearUserTOTPFailures(ctx, userID)
}

func (s *Store) DeleteUserTOTP(ctx context.Context, userID pgtype.UUID) error {
	return s.queries.DeleteUserTOTP(ctx, userID)
}

func matched(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
