package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Display name bounds (PF-1): optional free text, counted in characters.
const (
	minDisplayNameLength = 1
	maxDisplayNameLength = 64
)

// UpdateProfile replaces the account's display name. A nil name, or one that
// is empty after trimming, clears it. Anything else must be 1-64 characters
// after trimming or ErrDisplayNameInvalid answers.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, displayName *string) (*User, error) {
	name, err := normalizeDisplayName(displayName)
	if err != nil {
		return nil, err
	}

	user, err := s.store.UpdateUserDisplayName(ctx, sqlc.UpdateUserDisplayNameParams{
		ID:          pgUUID(userID),
		DisplayName: name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: update display name: %w", err)
	}

	s.logger.Info("auth: profile updated", "user_id", userID)
	return toUser(user), nil
}

// normalizeDisplayName trims the name; nil or blank clears (nil), otherwise
// the trimmed value must be 1-64 characters of storable text. Anything that
// is not valid UTF-8, any control character (this covers NUL, newlines and
// tabs), and any invisible format character (unicode.Cf covers the zero-width
// and bidi-override ranges U+200B-U+200F, U+202A-U+202E, U+2066-U+2069 and
// U+FEFF) is rejected: Postgres would refuse NUL with a 500, and the rest
// have no place in a rendered name.
func normalizeDisplayName(displayName *string) (*string, error) {
	if displayName == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*displayName)
	if trimmed == "" {
		return nil, nil
	}
	if !utf8.ValidString(trimmed) {
		return nil, ErrDisplayNameInvalid
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return nil, ErrDisplayNameInvalid
		}
	}
	if utf8.RuneCountInString(trimmed) < minDisplayNameLength || utf8.RuneCountInString(trimmed) > maxDisplayNameLength {
		return nil, ErrDisplayNameInvalid
	}
	return &trimmed, nil
}

// ChangePassword replaces the account's password and ends every other session.
// When the account has a password the current one must match (a mismatch or a
// missing value answers ErrCurrentPasswordIncorrect with a 400, never a
// 401/403); passwordless (OAuth-created) accounts set one without it. The new
// password follows the same rules as registration. The hash update, the
// credential-version bump and the session purge commit in one transaction, and
// the returned pair is minted from the post-bump row, so the caller keeps
// working while every other chain is refused by Refresh (P-A5).
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword string) (*AuthResult, error) {
	if err := ValidatePassword(newPassword); err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}

	if user.PasswordHash != nil {
		if current == "" {
			return nil, ErrCurrentPasswordIncorrect
		}
		ok, err := s.verifyPassword(*user.PasswordHash, current)
		if err != nil {
			s.logger.Error("auth: verify password", "error", err)
			return nil, ErrCurrentPasswordIncorrect
		}
		if !ok {
			// Burn one more real verification so the failure costs the same
			// whatever the stored credential looked like.
			s.spendPasswordCheck(current)
			return nil, ErrCurrentPasswordIncorrect
		}
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return nil, err
	}

	updated, err := s.store.ChangeUserPassword(ctx, pgUUID(userID), user.PasswordHash, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent change moved the credential after the check: the
			// verified current password is stale.
			return nil, ErrCurrentPasswordIncorrect
		}
		return nil, fmt.Errorf("auth: change password: %w", err)
	}

	s.logger.Info("auth: password changed", "user_id", userID)
	// The new credential is already committed and every other session is
	// gone; if issuing the caller's pair fails now, it logs in again with
	// the new password rather than retrying the change.
	return s.issue(ctx, updated)
}
