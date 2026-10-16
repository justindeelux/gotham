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
)

// TOTP parameters (JUS-196): RFC 6238 with HMAC-SHA1, six digits and a 30-second
// step, the defaults every authenticator app assumes. Codes are accepted one
// step either side of the current one, which tolerates a skewed client clock;
// the step a code matched is recorded so a used window can never be replayed.
const (
	totpIssuer         = "Gotham"
	totpPeriod         = 30 * time.Second
	totpDigits         = 6
	totpSkewSteps      = 1
	totpSecretBytes    = 20 // 160-bit seed, RFC 6238 appendix B.
	recoveryCodeCount  = 10
	recoveryCodeLength = 10 // 10 base32 chars == 50 bits of entropy.
)

// recoveryAlphabet is Crockford-style base32 without the easily confused I, O,
// 0 and 1. It is exactly 32 characters, and 256 is a multiple of 32, so mapping
// a random byte with %32 is unbiased.
const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// totpBase32 is the unpadded base32 used for secrets and recovery codes.
var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// TOTPStatus is the account's two-factor state as the profile page renders it.
type TOTPStatus struct {
	Enabled                bool `json:"enabled"`
	RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
}

// TOTPEnrollment is a pending enrollment: the secret to enter (or scan) and the
// otpauth URI the QR encodes. The secret is not live until EnableTOTP confirms
// a code.
type TOTPEnrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

// SetSecretKey installs the credential-encryption key that seals TOTP secrets
// at rest (the same GOTHAM_SECRET_KEY the provider credentials use). It must be
// called before any enrollment; an empty key fails enrollment closed.
func (s *Service) SetSecretKey(secret string) {
	s.secretKey = secret
}

// TOTPStatus returns the account's two-factor state. An account that never
// enrolled has no row: enabled false, zero codes.
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

// BeginTOTPEnrollment mints a fresh secret and stores it as a pending
// enrollment. It refuses while TOTP is already enabled: the caller must disable
// it first, so a live second factor is never silently replaced. The returned
// secret grants nothing on its own — EnableTOTP must confirm it with a code and
// the account password.
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
	if err := s.store.UpsertUserTOTPSecret(ctx, id, sealed); err != nil {
		return nil, fmt.Errorf("auth: store totp secret: %w", err)
	}

	return &TOTPEnrollment{Secret: secret, OTPAuthURL: totpProvisioningURI(user.Email, secret)}, nil
}

// EnableTOTP confirms the pending secret with a code and turns two-factor on.
// The account password is required when one is set (the same rule as a password
// change); it returns the recovery codes, which are shown once and stored only
// as argon2id hashes. The enrollment code's step is recorded, so the code used
// here cannot be replayed at login.
func (s *Service) EnableTOTP(ctx context.Context, userID uuid.UUID, password, code string) ([]string, error) {
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
	step, ok := verifyTOTP(secret, code, s.now())
	if !ok {
		return nil, ErrInvalidCode
	}

	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	// Record one step below the confirmation code so a login within the same
	// 30-second window is not refused: the enrollment code is not a login, and
	// from here on any step at or after it is accepted exactly once.
	enabled, err := s.store.EnableUserTOTP(ctx, id, step-1, hashes)
	if err != nil {
		return nil, fmt.Errorf("auth: enable totp: %w", err)
	}
	if !enabled {
		// A concurrent enable (or disable) won; the pending secret is not live.
		return nil, ErrTOTPNotEnrolled
	}

	s.logger.Info("auth: two-factor enabled", "user_id", userID)
	return codes, nil
}

// DisableTOTP turns two-factor off after proving the password and a current
// TOTP or recovery code. Deleting the row discards the secret and every
// recovery code at once.
func (s *Service) DisableTOTP(ctx context.Context, userID uuid.UUID, password, code string) error {
	id := pgUUID(userID)
	if err := s.requireTOTPPassword(ctx, id, password); err != nil {
		return err
	}
	if err := s.verifySecondFactor(ctx, id, code); err != nil {
		return err
	}
	if err := s.store.DeleteUserTOTP(ctx, id); err != nil {
		return fmt.Errorf("auth: disable totp: %w", err)
	}
	s.logger.Info("auth: two-factor disabled", "user_id", userID)
	return nil
}

// RegenerateTOTPRecoveryCodes replaces the account's recovery codes after
// proving the password and a current TOTP or recovery code. Every previous code
// is discarded, so a leaked old set cannot be used afterwards.
func (s *Service) RegenerateTOTPRecoveryCodes(ctx context.Context, userID uuid.UUID, password, code string) ([]string, error) {
	id := pgUUID(userID)
	if err := s.requireTOTPPassword(ctx, id, password); err != nil {
		return nil, err
	}
	if err := s.verifySecondFactor(ctx, id, code); err != nil {
		return nil, err
	}

	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	replaced, err := s.store.ReplaceUserTOTPRecoveryCodes(ctx, id, hashes)
	if err != nil {
		return nil, fmt.Errorf("auth: replace recovery codes: %w", err)
	}
	if !replaced {
		return nil, ErrTOTPNotConfigured
	}

	s.logger.Info("auth: recovery codes regenerated", "user_id", userID)
	return codes, nil
}

// CompleteMFALogin redeems the half-authenticated token from the first login
// step together with a TOTP or recovery code, and only then mints the session.
// A token that is expired, not an MFA token, or bound to a superseded
// credential version is refused with ErrInvalidCredentials.
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
	// A password reset between the two steps bumps the credential version and
	// ends the half-authenticated state with it.
	if user.CredentialVersion != claims.CredentialVersion {
		return nil, ErrInvalidCredentials
	}

	if err := s.verifySecondFactor(ctx, id, code); err != nil {
		return nil, err
	}

	return s.issue(ctx, user, meta)
}

// requireTOTPPassword proves the account's password when it has one, exactly
// like a password change does (a mismatch or an absent value answers
// ErrCurrentPasswordIncorrect with a 400, never a 401/403). A passwordless
// (OAuth-created) account proves itself with its interactive session alone.
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
		// Burn one more real verification so the failure costs the same
		// whatever the stored credential looked like.
		s.spendPasswordCheck(password)
		return ErrCurrentPasswordIncorrect
	}
	return nil
}

// verifySecondFactor accepts a current TOTP code or an unused recovery code for
// an account with TOTP enabled. A TOTP code advances the stored step (replay
// guard); a recovery code is consumed (single use). Both writes are guarded and
// atomic, so two concurrent requests cannot both redeem the same code.
func (s *Service) verifySecondFactor(ctx context.Context, userID pgtype.UUID, code string) error {
	row, err := s.store.GetUserTOTP(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTOTPNotConfigured
		}
		return fmt.Errorf("auth: get totp: %w", err)
	}
	if !row.Enabled {
		return ErrTOTPNotConfigured
	}

	secret, err := s.openTOTPSecret(row.SecretEnc)
	if err != nil {
		return err
	}
	if step, ok := verifyTOTP(secret, code, s.now()); ok {
		advanced, err := s.store.AdvanceUserTOTPStep(ctx, userID, step)
		if err != nil {
			return fmt.Errorf("auth: record totp step: %w", err)
		}
		if advanced {
			return nil
		}
		// The step was already used: a replayed code is not a second factor.
		return ErrInvalidCode
	}

	for _, hash := range row.RecoveryCodes {
		ok, err := VerifyPassword(hash, normalizeRecoveryCode(code))
		if err != nil || !ok {
			continue
		}
		consumed, err := s.store.ConsumeRecoveryCode(ctx, userID, hash)
		if err != nil {
			return fmt.Errorf("auth: consume recovery code: %w", err)
		}
		if consumed {
			return nil
		}
		return ErrInvalidCode
	}
	return ErrInvalidCode
}

// openTOTPSecret unseals a stored TOTP secret. A wrong key (for example a
// restarted instance that lost an ephemeral secret) fails closed.
func (s *Service) openTOTPSecret(sealed string) (string, error) {
	secret, err := providers.OpenSecret(s.secretKey, sealed)
	if err != nil {
		return "", fmt.Errorf("auth: open totp secret: %w", err)
	}
	return secret, nil
}

// generateTOTPSecret returns a fresh base32 (unpadded) seed.
func generateTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate totp secret: %w", err)
	}
	return totpBase32.EncodeToString(buf), nil
}

// totpProvisioningURI builds the otpauth:// URI an authenticator scans. It
// fixes the parameters the service verifies with, so the app cannot drift.
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

// verifyTOTP reports whether code is the RFC 6238 code for secret at at, and
// the step it matched. The comparison is constant time.
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

// totpCode computes the RFC 6238 code for key at counter: HMAC-SHA1 over the
// big-endian counter, dynamic truncation, modulo 10^digits.
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

// decodeTOTPSecret parses a base32 secret, tolerating absent padding and
// lowercase input (a manually typed secret).
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

// normalizeTOTPCode keeps only the digits of a submitted code, so "123 456"
// and "123456" are the same.
func normalizeTOTPCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// newRecoveryCodes mints recoveryCodeCount fresh codes and their argon2id
// hashes. The plaintext is returned once for display; only the hashes are
// stored. The codes are 50-bit random, which a fast hash would already make
// infeasible to crack offline, but reusing the vetted password hasher keeps one
// crypto primitive in the package and costs only the one-time hashing here.
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

// generateRecoveryCode returns a formatted recovery code ("XXXXX-XXXXX").
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

// normalizeRecoveryCode uppercases a submitted recovery code and drops its
// separators, so "abcde-fghjk" and "ABCDE FGHJK" both match the canonical
// (separator-free) form that is hashed and stored.
func normalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
