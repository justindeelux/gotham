package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTOTPRFC6238Vectors checks the generator against the RFC 6238 appendix B
// SHA-1 reference values (the eight-digit values truncated to the six digits
// this service issues), verifying the secret encoding, the counter derivation
// and the dynamic truncation at once.
func TestTOTPRFC6238Vectors(t *testing.T) {
	// base32("12345678901234567890"), the appendix B seed.
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}

	for _, vector := range vectors {
		at := time.Unix(vector.unix, 0)
		step, ok := verifyTOTP(secret, vector.want, at)
		if !ok {
			t.Errorf("verifyTOTP(secret, %q, %d) rejected the RFC vector", vector.want, vector.unix)
			continue
		}
		if want := vector.unix / int64(totpPeriod/time.Second); step != want {
			t.Errorf("verifyTOTP matched step %d at %d, want %d", step, vector.unix, want)
		}
	}
}

// TestTOTPSkewWindow: a code one step either side of "now" is accepted (a
// skewed client clock), while one two steps away is not.
func TestTOTPSkewWindow(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generateTOTPSecret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)

	for _, skew := range []time.Duration{-totpPeriod, totpPeriod} {
		code := codeForSecret(t, secret, now.Add(skew))
		if _, ok := verifyTOTP(secret, code, now); !ok {
			t.Errorf("code at skew %s was rejected, want accepted", skew)
		}
	}

	for _, skew := range []time.Duration{-2 * totpPeriod, 2 * totpPeriod} {
		code := codeForSecret(t, secret, now.Add(skew))
		if _, ok := verifyTOTP(secret, code, now); ok {
			t.Errorf("code at skew %s was accepted, want rejected", skew)
		}
	}
}

// TestTOTPCodeFormatting: separators are tolerated, and a code with the wrong
// digit count or a non-digit never verifies (so a recovery code cannot be
// mistaken for a TOTP code).
func TestTOTPCodeFormatting(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generateTOTPSecret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	code := codeForSecret(t, secret, now)

	spaced := code[:3] + " " + code[3:]
	if _, ok := verifyTOTP(secret, spaced, now); !ok {
		t.Errorf("spaced code %q was rejected, want accepted", spaced)
	}
	if _, ok := verifyTOTP(secret, "54321", now); ok {
		t.Errorf("five-digit code was accepted, want rejected")
	}
	if _, ok := verifyTOTP(secret, "ABCDEF", now); ok {
		t.Errorf("non-numeric code was accepted, want rejected")
	}
}

// TestRecoveryCodesAreRandomAndHasheable: generated codes use the unambiguous
// alphabet, are the advertised length, and normalize to a separator-free form
// that verifies against the stored argon2id hash.
func TestRecoveryCodes(t *testing.T) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		t.Fatalf("newRecoveryCodes: %v", err)
	}
	if len(codes) != recoveryCodeCount || len(hashes) != recoveryCodeCount {
		t.Fatalf("newRecoveryCodes returned %d codes / %d hashes, want %d", len(codes), len(hashes), recoveryCodeCount)
	}

	seen := make(map[string]bool, len(codes))
	for i, code := range codes {
		if got := len(strings.ReplaceAll(code, "-", "")); got != recoveryCodeLength {
			t.Errorf("code %q has %d characters, want %d", code, got, recoveryCodeLength)
		}
		for _, r := range strings.ReplaceAll(code, "-", "") {
			if !strings.ContainsRune(recoveryAlphabet, r) {
				t.Errorf("code %q uses %q, outside the unambiguous alphabet", code, r)
			}
		}
		if seen[code] {
			t.Errorf("duplicate recovery code %q", code)
		}
		seen[code] = true

		// A lowercase, spaced submission still matches the stored hash.
		submitted := strings.ToLower(code[:5]) + " " + strings.ToLower(code[6:])
		ok, err := VerifyPassword(hashes[i], normalizeRecoveryCode(submitted))
		if err != nil || !ok {
			t.Errorf("stored hash for %q did not verify its normalized form: ok=%v err=%v", code, ok, err)
		}
	}
}

// TestTOTPSecretDecodingToleratesManualEntry: a user typing the secret in
// lowercase or with spaces still decodes, and padding is optional.
func TestTOTPSecretDecodingToleratesManualEntry(t *testing.T) {
	if _, err := decodeTOTPSecret("gezd gnbv gy3t qojq gezd gnbv gy3t qojq"); err != nil {
		t.Fatalf("decodeTOTPSecret rejected a manual entry: %v", err)
	}
	if _, err := decodeTOTPSecret("!!!"); err == nil {
		t.Fatalf("decodeTOTPSecret accepted an invalid secret")
	}
}

// codeForSecret returns the TOTP code for secret at at. It shares the
// production decoder and generator so the tests exercise the same path.
func codeForSecret(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		t.Fatalf("decodeTOTPSecret: %v", err)
	}
	return totpCode(key, uint64(at.Unix()/int64(totpPeriod/time.Second)))
}

// TestMFATokenIsNotAnAccessToken: the half-authenticated token carries the MFA
// purpose, so it is refused as an access token (and therefore as a WS token),
// while an access token is refused as an MFA token.
func TestMFATokenIsNotAnAccessToken(t *testing.T) {
	signer, err := NewSigner(nil, nil)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	const version = int32(7)

	mfa, err := signer.IssueMFAToken(uuid.New(), version)
	if err != nil {
		t.Fatalf("IssueMFAToken: %v", err)
	}
	if _, err := signer.VerifyAccessToken(mfa); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyAccessToken(mfa token) = %v, want ErrInvalidToken", err)
	}
	claims, err := signer.VerifyMFAToken(mfa)
	if err != nil {
		t.Fatalf("VerifyMFAToken: %v", err)
	}
	if claims.CredentialVersion != version {
		t.Errorf("MFA token credential version = %d, want %d", claims.CredentialVersion, version)
	}

	access, _, err := signer.IssueAccessToken(uuid.New(), "user", uuid.Nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if _, err := signer.VerifyMFAToken(access); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyMFAToken(access token) = %v, want ErrInvalidToken", err)
	}
	if _, err := signer.VerifyAccessToken(access); err != nil {
		t.Fatalf("VerifyAccessToken(access token) = %v, want nil", err)
	}
}
