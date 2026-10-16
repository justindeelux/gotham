package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTOTPEnrollChallengeAndRecovery covers the end-to-end flow: enroll,
// confirm, be challenged at login without any token being issued, complete with
// a TOTP code (no replay), complete with a recovery code (single use), and
// regenerate (which invalidates the old set).
func TestTOTPEnrollChallengeAndRecovery(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-flow")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)

	// Drive the clock explicitly: the replay guard is measured in 30-second
	// steps, so the test advances the service's clock instead of sleeping.
	clock := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return clock }

	// Before enrollment a login issues a token pair directly.
	plain, err := svc.Login(ctx, email, password, SessionMeta{})
	if err != nil {
		t.Fatalf("Login before enrollment: %v", err)
	}
	if plain.MFARequired || plain.AccessToken == "" {
		t.Fatalf("login before enrollment = %+v, want a plain token pair", plain)
	}

	// Enroll and confirm.
	enrollment, err := svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	if enrollment.Secret == "" || enrollment.OTPAuthURL == "" {
		t.Fatalf("enrollment = %+v, want a secret and otpauth URI", enrollment)
	}
	recoveryCodes, err := svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), uuid.Nil)
	if err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	if len(recoveryCodes) != recoveryCodeCount {
		t.Fatalf("EnableTOTP returned %d recovery codes, want %d", len(recoveryCodes), recoveryCodeCount)
	}

	status, err := svc.TOTPStatus(ctx, userID)
	if err != nil {
		t.Fatalf("TOTPStatus: %v", err)
	}
	if !status.Enabled || status.RecoveryCodesRemaining != recoveryCodeCount {
		t.Fatalf("status = %+v, want enabled with %d codes", status, recoveryCodeCount)
	}

	// A login now returns the half-authenticated challenge: no session, access
	// token, refresh token or (the WS token is the access token) WS token.
	before, err := st.ListSessionsByUser(ctx, pgUUID(userID), clock)
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	challenge, err := svc.Login(ctx, email, password, SessionMeta{})
	if err != nil {
		t.Fatalf("Login with TOTP: %v", err)
	}
	if !challenge.MFARequired || challenge.MFAToken == "" {
		t.Fatalf("challenge = %+v, want mfa_required with a token", challenge)
	}
	if challenge.AccessToken != "" || challenge.RefreshToken != "" || challenge.ExpiresIn != 0 {
		t.Fatalf("challenge issued a token pair: %+v", challenge)
	}
	if _, err := svc.VerifyAccessToken(challenge.MFAToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("the half-authenticated token verified as an access token: %v", err)
	}
	after, err := st.ListSessionsByUser(ctx, pgUUID(userID), clock)
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("a session was created before the second factor: %d -> %d", len(before), len(after))
	}

	// Complete with the current TOTP code, then prove it cannot be replayed.
	code := codeForSecret(t, enrollment.Secret, clock)
	full, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, code, SessionMeta{})
	if err != nil {
		t.Fatalf("CompleteMFALogin: %v", err)
	}
	if full.AccessToken == "" || full.RefreshToken == "" {
		t.Fatalf("CompleteMFALogin returned an incomplete pair: %+v", full)
	}
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, code, SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("replay of the accepted code = %v, want ErrInvalidCode", err)
	}

	// A wrong code is refused.
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, "000000", SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("wrong code = %v, want ErrInvalidCode", err)
	}

	// The next step's code works once the clock advances.
	clock = clock.Add(totpPeriod)
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, codeForSecret(t, enrollment.Secret, clock), SessionMeta{}); err != nil {
		t.Fatalf("CompleteMFALogin with the next step's code: %v", err)
	}

	// A recovery code works once and only once.
	recovery := recoveryCodes[0]
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, recovery, SessionMeta{}); err != nil {
		t.Fatalf("CompleteMFALogin with a recovery code: %v", err)
	}
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, recovery, SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("reused recovery code = %v, want ErrInvalidCode", err)
	}

	// Regenerating invalidates every previous code.
	clock = clock.Add(totpPeriod)
	if _, err := svc.RegenerateTOTPRecoveryCodes(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), uuid.Nil); err != nil {
		t.Fatalf("RegenerateTOTPRecoveryCodes: %v", err)
	}
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, recoveryCodes[1], SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("a pre-regeneration recovery code = %v, want ErrInvalidCode", err)
	}

	// Disable with the password and a fresh TOTP code; login is plain again.
	clock = clock.Add(totpPeriod)
	if err := svc.DisableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), uuid.Nil); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	afterDisable, err := svc.Login(ctx, email, password, SessionMeta{})
	if err != nil {
		t.Fatalf("Login after disable: %v", err)
	}
	if afterDisable.MFARequired || afterDisable.AccessToken == "" {
		t.Fatalf("login after disable = %+v, want a plain token pair", afterDisable)
	}
}

// TestTOTPProfileActionsRequirePassword: enabling, disabling and regenerating
// all refuse a wrong current password, so a stolen interactive session cannot
// manage the second factor alone.
func TestTOTPProfileActionsRequirePassword(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-password")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)

	enrollment, err := svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}

	if _, err := svc.EnableTOTP(ctx, userID, "wrong-password", codeForSecret(t, enrollment.Secret, time.Now()), uuid.Nil); !errors.Is(err, ErrCurrentPasswordIncorrect) {
		t.Fatalf("EnableTOTP with a wrong password = %v, want ErrCurrentPasswordIncorrect", err)
	}
	// Enrolling again while pending is allowed (a fresh secret); while enabled
	// it is refused.
	if _, err := svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, time.Now()), uuid.Nil); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	if _, err := svc.BeginTOTPEnrollment(ctx, userID); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Fatalf("BeginTOTPEnrollment while enabled = %v, want ErrTOTPAlreadyEnabled", err)
	}
	if err := svc.DisableTOTP(ctx, userID, "wrong-password", codeForSecret(t, enrollment.Secret, time.Now()), uuid.Nil); !errors.Is(err, ErrCurrentPasswordIncorrect) {
		t.Fatalf("DisableTOTP with a wrong password = %v, want ErrCurrentPasswordIncorrect", err)
	}
	if _, err := svc.RegenerateTOTPRecoveryCodes(ctx, userID, "wrong-password", codeForSecret(t, enrollment.Secret, time.Now()), uuid.Nil); !errors.Is(err, ErrCurrentPasswordIncorrect) {
		t.Fatalf("RegenerateTOTPRecoveryCodes with a wrong password = %v, want ErrCurrentPasswordIncorrect", err)
	}
}

// TestTOTPResetClearsSecondFactor: the admin password reset (the documented
// path for a lost device) clears the TOTP configuration, so the account signs in
// with the password alone afterwards.
func TestTOTPResetClearsSecondFactor(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-reset")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)

	enrollment, err := svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	if _, err := svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, time.Now()), uuid.Nil); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	user, err := st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	newHash, err := HashPassword("another-s3cret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := st.ResetUserPassword(ctx, user.ID, user.Email, newHash); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}

	status, err := svc.TOTPStatus(ctx, userID)
	if err != nil {
		t.Fatalf("TOTPStatus: %v", err)
	}
	if status.Enabled {
		t.Fatalf("status after reset = %+v, want disabled", status)
	}

	loggedIn, err := svc.Login(ctx, email, "another-s3cret-password", SessionMeta{})
	if err != nil {
		t.Fatalf("Login after reset: %v", err)
	}
	if loggedIn.MFARequired {
		t.Fatalf("login after reset still challenges the second factor: %+v", loggedIn)
	}
}
