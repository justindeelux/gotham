package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 mandates HMAC-SHA1 for authenticator compatibility.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
)

const (
	totpIssuer         = "Gotham"
	totpPeriod         = 30 * time.Second
	totpDigits         = 6
	totpSkewSteps      = 1
	totpSecretBytes    = 20
	recoveryCodeCount  = 10
	recoveryCodeLength = 10
)

const (
	totpMaxFailures   = 5
	totpLockoutWindow = 5 * time.Minute
)

const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

type TOTPStatus struct {
	Enabled                bool `json:"enabled"`
	RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
}

type TOTPEnrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

func (s *Service) SetSecretKey(secret string) {
	s.secretKey = secret
}

func (s *Service) TOTPStatus(ctx context.Context, userID uuid.UUID) (*TOTPStatus, error) {
	row, err := s.store.GetUserTOTP(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &TOTPStatus{}, nil
		}
		return nil, fmt.Errorf("auth: get totp: %w", err)
	}
	return &TOTPStatus{Enabled: row.Enabled, RecoveryCodesRemaining: len(row.RecoveryCodes)}, nil
}

func (s *Service) BeginTOTPEnrollment(ctx context.Context, userID uuid.UUID) (*TOTPEnrollment, error) {
	id := pgUUID(userID)

	if row, err := s.store.GetUserTOTP(ctx, id); err == nil {
		if row.Enabled {
			return nil, ErrTOTPAlreadyEnabled
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("auth: get totp: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}

	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := providers.SealSecret(s.secretKey, secret)
	if err != nil {
		return nil, fmt.Errorf("auth: seal totp secret: %w", err)
	}
	stored, err := s.store.UpsertUserTOTPSecret(ctx, id, sealed)
	if err != nil {
		return nil, fmt.Errorf("auth: store totp secret: %w", err)
	}
	if !stored {
		return nil, ErrTOTPAlreadyEnabled
	}

	return &TOTPEnrollment{Secret: secret, OTPAuthURL: totpProvisioningURI(user.Email, secret)}, nil
}

func (s *Service) EnableTOTP(ctx context.Context, userID uuid.UUID, password, code string, currentSessionID uuid.UUID) ([]string, error) {
	id := pgUUID(userID)
	if err := s.requireTOTPPassword(ctx, id, password); err != nil {
		return nil, err
	}

	row, err := s.store.GetUserTOTP(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTOTPNotEnrolled
		}
		return nil, fmt.Errorf("auth: get totp: %w", err)
	}
	if row.Enabled {
		return nil, ErrTOTPAlreadyEnabled
	}

	secret, err := s.openTOTPSecret(row.SecretEnc)
	if err != nil {
		return nil, err
	}
	if err := s.beginTOTPAttempt(ctx, id); err != nil {
		return nil, err
	}
	step, ok := verifyTOTP(secret, code, s.now())
	if !ok {
		return nil, ErrInvalidCode
	}

	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	err = s.store.InTx(ctx, func(tx *store.Store) error {
		enabled, err := tx.EnableUserTOTP(ctx, id, step-1, hashes)
		if err != nil {
			return fmt.Errorf("auth: enable totp: %w", err)
		}
		if !enabled {
			return ErrTOTPNotEnrolled
		}
		return s.revokeOtherSessions(ctx, tx, userID, currentSessionID)
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info("auth: two-factor enabled", "user_id", userID)
	return codes, nil
}

func (s *Service) DisableTOTP(ctx context.Context, userID uuid.UUID, password, code string, currentSessionID uuid.UUID) error {
	id := pgUUID(userID)
	if err := s.requireTOTPPassword(ctx, id, password); err != nil {
		return err
	}
	if _, err := s.verifySecondFactor(ctx, id, code); err != nil {
		return err
	}
	err := s.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteUserTOTP(ctx, id); err != nil {
			return fmt.Errorf("auth: disable totp: %w", err)
		}
		return s.revokeOtherSessions(ctx, tx, userID, currentSessionID)
	})
	if err != nil {
		return err
	}
	s.logger.Info("auth: two-factor disabled", "user_id", userID)
	return nil
}

func (s *Service) RegenerateTOTPRecoveryCodes(ctx context.Context, userID uuid.UUID, password, code string, currentSessionID uuid.UUID) ([]string, error) {
	id := pgUUID(userID)
	if err := s.requireTOTPPassword(ctx, id, password); err != nil {
		return nil, err
	}
	if _, err := s.verifySecondFactor(ctx, id, code); err != nil {
		return nil, err
	}

	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	err = s.store.InTx(ctx, func(tx *store.Store) error {
		replaced, err := tx.ReplaceUserTOTPRecoveryCodes(ctx, id, hashes)
		if err != nil {
			return fmt.Errorf("auth: replace recovery codes: %w", err)
		}
		if !replaced {
			return ErrTOTPNotConfigured
		}
		return s.revokeOtherSessions(ctx, tx, userID, currentSessionID)
	})
	if err != nil {
		return nil, err
	}

	s.logger.Info("auth: recovery codes regenerated", "user_id", userID)
	return codes, nil
}

func (s *Service) CompleteMFALogin(ctx context.Context, mfaToken, code string, meta SessionMeta) (*AuthResult, error) {
	claims, err := s.signer.VerifyMFAToken(mfaToken)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	id := pgUUID(userID)
	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	if user.CredentialVersion != claims.CredentialVersion {
		return nil, ErrInvalidCredentials
	}

	usedRecovery, err := s.verifySecondFactor(ctx, id, code)
	if err != nil {
		return nil, err
	}
	if !usedRecovery {
		return s.issue(ctx, user, meta)
	}
	var result *AuthResult
	err = s.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteUserSessions(ctx, id); err != nil {
			return fmt.Errorf("auth: revoke sessions after recovery code: %w", err)
		}
		issued, err := s.issueWith(ctx, tx, user, meta)
		if err != nil {
			return err
		}
		result = issued
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info("auth: sessions revoked after recovery code", "user_id", userID)
	return result, nil
}

func (s *Service) requireTOTPPassword(ctx context.Context, userID pgtype.UUID, password string) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return fmt.Errorf("auth: get user: %w", err)
	}
	if user.PasswordHash == nil {
		return nil
	}
	if password == "" {
		return ErrCurrentPasswordIncorrect
	}
	ok, err := s.verifyPassword(*user.PasswordHash, password)
	if err != nil {
		s.logger.Error("auth: verify password", "error", err)
		return ErrCurrentPasswordIncorrect
	}
	if !ok {
		s.spendPasswordCheck(password)
		return ErrCurrentPasswordIncorrect
	}
	return nil
}

func (s *Service) verifySecondFactor(ctx context.Context, userID pgtype.UUID, code string) (bool, error) {
	row, err := s.store.GetUserTOTP(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrTOTPNotConfigured
		}
		return false, fmt.Errorf("auth: get totp: %w", err)
	}
	if !row.Enabled {
		return false, ErrTOTPNotConfigured
	}

	secret, err := s.openTOTPSecret(row.SecretEnc)
	if err != nil {
		return false, err
	}
	if err := s.beginTOTPAttempt(ctx, userID); err != nil {
		return false, err
	}
	if step, ok := verifyTOTP(secret, code, s.now()); ok {
		advanced, err := s.store.AdvanceUserTOTPStep(ctx, userID, step)
		if err != nil {
			return false, fmt.Errorf("auth: record totp step: %w", err)
		}
		if advanced {
			s.clearTOTPFailures(ctx, userID)
			return false, nil
		}
		return false, ErrInvalidCode
	}

	normalized := normalizeRecoveryCode(code)
	if len(normalized) == recoveryCodeLength {
		for _, hash := range row.RecoveryCodes {
			ok, err := VerifyPassword(hash, normalized)
			if err != nil || !ok {
				continue
			}
			consumed, err := s.store.ConsumeRecoveryCode(ctx, userID, hash)
			if err != nil {
				return false, fmt.Errorf("auth: consume recovery code: %w", err)
			}
			if consumed {
				s.clearTOTPFailures(ctx, userID)
				return true, nil
			}
			return false, ErrInvalidCode
		}
	}
	return false, ErrInvalidCode
}

func (s *Service) beginTOTPAttempt(ctx context.Context, userID pgtype.UUID) error {
	now := s.now()
	reserved, err := s.store.BeginUserTOTPAttempt(ctx, userID, totpMaxFailures, now, now.Add(totpLockoutWindow))
	if err != nil {
		return fmt.Errorf("auth: begin totp attempt: %w", err)
	}
	if !reserved {
		return ErrTOTPLocked
	}
	return nil
}

func (s *Service) clearTOTPFailures(ctx context.Context, userID pgtype.UUID) {
	if err := s.store.ClearUserTOTPFailures(ctx, userID); err != nil {
		s.logger.Error("auth: clear totp failures", "error", err)
	}
}

func (s *Service) revokeOtherSessions(ctx context.Context, st *store.Store, userID, currentSessionID uuid.UUID) error {
	if currentSessionID == uuid.Nil {
		return nil
	}
	deleted, live, err := st.RevokeOtherSessionsGuarded(ctx, pgUUID(userID), pgUUID(currentSessionID))
	if err != nil {
		return fmt.Errorf("auth: revoke other sessions: %w", err)
	}
	if !live {
		s.logger.Warn("auth: current session not live, nothing revoked", "user_id", userID)
		return nil
	}
	s.logger.Info("auth: other sessions revoked", "user_id", userID, "count", deleted)
	return nil
}

func (s *Service) openTOTPSecret(sealed string) (string, error) {
	secret, err := providers.OpenSecret(s.secretKey, sealed)
	if err != nil {
		return "", fmt.Errorf("auth: open totp secret: %w", err)
	}
	return secret, nil
}

func generateTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate totp secret: %w", err)
	}
	return totpBase32.EncodeToString(buf), nil
}

func totpProvisioningURI(account, secret string) string {
	query := url.Values{
		"secret":    {secret},
		"issuer":    {totpIssuer},
		"algorithm": {"SHA1"},
		"digits":    {fmt.Sprint(totpDigits)},
		"period":    {fmt.Sprint(int(totpPeriod / time.Second))},
	}
	label := url.PathEscape(totpIssuer + ":" + account)
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func verifyTOTP(secret, code string, at time.Time) (int64, bool) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return 0, false
	}
	code = normalizeTOTPCode(code)
	if len(code) != totpDigits {
		return 0, false
	}

	counter := at.Unix() / int64(totpPeriod/time.Second)
	for skew := int64(-totpSkewSteps); skew <= totpSkewSteps; skew++ {
		step := counter + skew
		if step < 0 {
			continue
		}
		want := totpCode(key, uint64(step))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func totpCode(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, truncated%1_000_000)
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	if pad := len(normalized) % 8; pad != 0 {
		normalized += strings.Repeat("=", 8-pad)
	}
	key, err := base32.StdEncoding.DecodeString(normalized)
	if err != nil {
		return nil, fmt.Errorf("auth: decode totp secret: %w", err)
	}
	return key, nil
}

func normalizeTOTPCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func newRecoveryCodes() (codes []string, hashes []string, err error) {
	codes = make([]string, recoveryCodeCount)
	hashes = make([]string, recoveryCodeCount)
	for i := range codes {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		hash, err := HashPassword(normalizeRecoveryCode(code))
		if err != nil {
			return nil, nil, err
		}
		codes[i] = code
		hashes[i] = hash
	}
	return codes, hashes, nil
}

func generateRecoveryCode() (string, error) {
	buf := make([]byte, recoveryCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate recovery code: %w", err)
	}
	var b strings.Builder
	for i, value := range buf {
		if i == recoveryCodeLength/2 {
			b.WriteByte('-')
		}
		b.WriteByte(recoveryAlphabet[int(value)%len(recoveryAlphabet)])
	}
	return b.String(), nil
}

func normalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
