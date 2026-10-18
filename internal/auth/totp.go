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

// Second-factor lockout (JUS-196 fix round 1): consecutive wrong codes — TOTP
// or recovery, from any source — lock the account for a few minutes. The
// counter lives on the account's TOTP row, so rotating the source address, the
// half-authenticated token or an IPv6 /64 cannot reset it.
const (
	totpMaxFailures   = 5
	totpLockoutWindow = 5 * time.Minute
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
	stored, err := s.store.UpsertUserTOTPSecret(ctx, id, sealed)
	if err != nil {
		return nil, fmt.Errorf("auth: store totp secret: %w", err)
	}
	if !stored {
		// A concurrent enable made the row live between the check above and
		// here; the pending secret must not replace it (JUS-198).
		return nil, ErrTOTPAlreadyEnabled
	}

	return &TOTPEnrollment{Secret: secret, OTPAuthURL: totpProvisioningURI(user.Email, secret)}, nil
}

// EnableTOTP confirms the pending secret with a code and turns two-factor on.
// The account password is required when one is set (the same rule as a password
// change); it returns the recovery codes, which are shown once and stored only
// as argon2id hashes. The enrollment code's step is recorded, so the code used
// here cannot be replayed at login. Every other session is ended once the
// second factor is live, so a stolen pre-enrollment chain cannot outlive it;
// currentSessionID is the caller's own session, which is kept.
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
	// Count this attempt before checking the code, so a burst of guesses cannot
	// all slip past the lock (JUS-198); a locked account is refused here.
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
	// Record one step below the confirmation code so a login within the same
	// 30-second window is not refused: the enrollment code is not a login, and
	// from here on any step at or after it is accepted exactly once.
	err = s.store.InTx(ctx, func(tx *store.Store) error {
		enabled, err := tx.EnableUserTOTP(ctx, id, step-1, hashes)
		if err != nil {
			return fmt.Errorf("auth: enable totp: %w", err)
		}
		if !enabled {
			// A concurrent enable (or disable) won; the pending secret is not live.
			return ErrTOTPNotEnrolled
		}
		// The second factor is committed; retire every chain that predates it,
		// in the same transaction so a revocation failure rolls the enable back.
		return s.revokeOtherSessions(ctx, tx, userID, currentSessionID)
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info("auth: two-factor enabled", "user_id", userID)
	return codes, nil
}

// DisableTOTP turns two-factor off after proving the password and a current
// TOTP or recovery code. Deleting the row discards the secret and every
// recovery code at once, and every other session is ended so a chain that
// relied on the second factor being absent does not survive the change.
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
		// Revoke the other chains in the same transaction, so a revocation
		// failure aborts the disable instead of leaving old sessions alive.
		return s.revokeOtherSessions(ctx, tx, userID, currentSessionID)
	})
	if err != nil {
		return err
	}
	s.logger.Info("auth: two-factor disabled", "user_id", userID)
	return nil
}

// RegenerateTOTPRecoveryCodes replaces the account's recovery codes after
// proving the password and a current TOTP or recovery code. Every previous code
// is discarded, so a leaked old set cannot be used afterwards, and every other
// session is ended so a chain that never saw the new set does not survive.
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
		// Revoke the other chains in the same transaction, so a revocation
		// failure aborts the regeneration.
		return s.revokeOtherSessions(ctx, tx, userID, currentSessionID)
	})
	if err != nil {
		return nil, err
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

	usedRecovery, err := s.verifySecondFactor(ctx, id, code)
	if err != nil {
		return nil, err
	}
	if !usedRecovery {
		return s.issue(ctx, user, meta)
	}
	// A recovery code means the authenticator is gone, so every pre-existing
	// chain is suspect: end them all and mint the caller's new session in one
	// transaction, so a revocation failure cannot leave an old chain alive next
	// to the new one (JUS-198).
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
// an account with TOTP enabled, reporting whether the value that matched was a
// recovery code. A TOTP code advances the stored step (replay guard); a
// recovery code is consumed (single use). Both writes are guarded and atomic,
// so two concurrent requests cannot both redeem the same code.
//
// Failures are counted on the account (not the caller's address or token) and
// lock it for a few minutes after totpMaxFailures consecutive misses; a correct
// code clears the counter. The attempt is counted before the code is checked
// (JUS-198), so a parallel burst cannot overshoot the threshold or slip a guess
// past an already-locked account.
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
		// The step was already used: a replayed code is not a second factor.
		return false, ErrInvalidCode
	}

	// Only a value shaped like a recovery code can be one, so a mistyped TOTP
	// code never pays for ten argon2id verifications.
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
	// The attempt is already counted; a wrong code just leaves it standing.
	return false, ErrInvalidCode
}

// beginTOTPAttempt reserves one second-factor attempt on the account's row
// before the code is checked: the count and the lockout deadline move in one
// atomic statement, so a parallel burst cannot all pass the check and overshoot
// the threshold (JUS-198). It answers ErrTOTPLocked while the lockout is still
// active, in which case the attempt was not counted. A correct code rolls the
// reservation back with clearTOTPFailures.
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

// clearTOTPFailures resets the counter after a correct code.
func (s *Service) clearTOTPFailures(ctx context.Context, userID pgtype.UUID) {
	if err := s.store.ClearUserTOTPFailures(ctx, userID); err != nil {
		s.logger.Error("auth: clear totp failures", "error", err)
	}
}

// revokeOtherSessions ends every live session of the account except the
// caller's, on the given store so it joins the caller's transaction: a failure
// fails the whole operation rather than leaving old sessions alive. A sid that
// is missing or no longer live means the caller's own row cannot be preserved,
// so nothing is deleted (a warning, not an error) rather than logging the
// active user out. The access tokens already issued still live out their short
// TTL, exactly as RevokeOtherSessions documents.
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
