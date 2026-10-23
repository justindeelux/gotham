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

const (
	minDisplayNameLength = 1
	maxDisplayNameLength = 64
)

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
		if unicode.IsControl(r) || isDeniedFormatChar(r) {
			return nil, ErrDisplayNameInvalid
		}
	}
	if utf8.RuneCountInString(trimmed) < minDisplayNameLength || utf8.RuneCountInString(trimmed) > maxDisplayNameLength {
		return nil, ErrDisplayNameInvalid
	}
	return &trimmed, nil
}

func isDeniedFormatChar(r rune) bool {
	switch r {
	case 0x200B,
		0x200E, 0x200F,
		0x2028, 0x2029,
		0x2060,
		0xFEFF:
		return true
	}
	switch {
	case r >= 0x202A && r <= 0x202E,
		r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword string, meta SessionMeta) (*AuthResult, error) {
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
			return nil, ErrCurrentPasswordIncorrect
		}
		return nil, fmt.Errorf("auth: change password: %w", err)
	}

	s.logger.Info("auth: password changed", "user_id", userID)
	return s.issue(ctx, updated, meta)
}
