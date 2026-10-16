package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// listSids returns the account's live session ids in an id->bool set.
func liveSids(t *testing.T, svc *Service, userID uuid.UUID, at time.Time) map[uuid.UUID]bool {
	t.Helper()
	rows, err := svc.store.ListSessionsByUser(context.Background(), pgUUID(userID), at)
	if err != nil {
		t.Fatalf("ListSessionsByUser: %v", err)
	}
	out := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		out[uuid.UUID(row.ID.Bytes)] = true
	}
	return out
}

// TestTOTPRevokesOtherSessions covers the three session-revocation rules for
// JUS-196: enabling and disabling keep the caller's session and end the rest,
// and redeeming a recovery code ends every pre-existing chain before minting
// the caller's new one.
func TestTOTPRevokesOtherSessions(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-sessions")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	// A real base time keeps the created sessions live as far as the guard's
	// DB-side now() is concerned; the test only ever needs a frozen clock so
	// the TOTP steps line up, not a specific date.
	clock := time.Now()
	svc.now = func() time.Time { return clock }

	registered, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)
	currentSession := sidOf(t, svc, registered.AccessToken)

	// A second, unrelated chain exists before enrollment.
	other, err := svc.Login(ctx, email, password, SessionMeta{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	otherSession := sidOf(t, svc, other.AccessToken)
	if !liveSids(t, svc, userID, clock)[otherSession] {
		t.Fatalf("the second login did not create a session")
	}

	enrollment, err := svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	if _, err = svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), currentSession); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	sids := liveSids(t, svc, userID, clock)
	if !sids[currentSession] || sids[otherSession] || len(sids) != 1 {
		t.Fatalf("sessions after enable = %v, want only the caller's", sids)
	}

	// Completing a TOTP login mints a third chain.
	challenge, err := svc.Login(ctx, email, password, SessionMeta{})
	if err != nil || !challenge.MFARequired {
		t.Fatalf("Login with TOTP = %+v, err %v", challenge, err)
	}
	clock = clock.Add(totpPeriod)
	full, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, codeForSecret(t, enrollment.Secret, clock), SessionMeta{})
	if err != nil {
		t.Fatalf("CompleteMFALogin: %v", err)
	}
	newSession := sidOf(t, svc, full.AccessToken)
	if len(liveSids(t, svc, userID, clock)) != 2 {
		t.Fatalf("expected two sessions after a TOTP login")
	}

	// Disabling keeps the caller's original session and ends the rest.
	clock = clock.Add(totpPeriod)
	if err := svc.DisableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), currentSession); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	sids = liveSids(t, svc, userID, clock)
	if !sids[currentSession] || sids[newSession] || len(sids) != 1 {
		t.Fatalf("sessions after disable = %v, want only the caller's", sids)
	}

	// Re-enable to exercise the recovery-code rule.
	enrollment, err = svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment (second): %v", err)
	}
	clock = clock.Add(totpPeriod)
	codes, err := svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), currentSession)
	if err != nil {
		t.Fatalf("EnableTOTP (second): %v", err)
	}

	challenge, err = svc.Login(ctx, email, password, SessionMeta{})
	if err != nil || !challenge.MFARequired {
		t.Fatalf("Login with TOTP (second) = %+v, err %v", challenge, err)
	}
	recovered, err := svc.CompleteMFALogin(ctx, challenge.MFAToken, codes[0], SessionMeta{})
	if err != nil {
		t.Fatalf("CompleteMFALogin with a recovery code: %v", err)
	}
	recoveredSession := sidOf(t, svc, recovered.AccessToken)
	sids = liveSids(t, svc, userID, clock)
	if !sids[recoveredSession] || sids[currentSession] || len(sids) != 1 {
		t.Fatalf("sessions after a recovery-code login = %v, want only the new one", sids)
	}
}
