package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTOTPEnrollChallengeAndRecovery(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-flow")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)

	clock := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return clock }

	plain, err := svc.Login(ctx, email, password, SessionMeta{})
	if err != nil {
		t.Fatalf("Login before enrollment: %v", err)
	}
	if plain.MFARequired || plain.AccessToken == "" {
		t.Fatalf("login before enrollment = %+v, want a plain token pair", plain)
	}

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

	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, "000000", SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("wrong code = %v, want ErrInvalidCode", err)
	}

	clock = clock.Add(totpPeriod)
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, codeForSecret(t, enrollment.Secret, clock), SessionMeta{}); err != nil {
		t.Fatalf("CompleteMFALogin with the next step's code: %v", err)
	}

	recovery := recoveryCodes[0]
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, recovery, SessionMeta{}); err != nil {
		t.Fatalf("CompleteMFALogin with a recovery code: %v", err)
	}
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, recovery, SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("reused recovery code = %v, want ErrInvalidCode", err)
	}

	clock = clock.Add(totpPeriod)
	if _, err := svc.RegenerateTOTPRecoveryCodes(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), uuid.Nil); err != nil {
		t.Fatalf("RegenerateTOTPRecoveryCodes: %v", err)
	}
	if _, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, recoveryCodes[1], SessionMeta{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("a pre-regeneration recovery code = %v, want ErrInvalidCode", err)
	}

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

func TestTOTPProfileActionsRequirePassword(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-password")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, SessionMeta{})
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

func TestTOTPResetClearsSecondFactor(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-reset")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, SessionMeta{})
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
