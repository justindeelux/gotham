package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// enrollTOTP registers an account, enrolls it and turns the second factor on,
// returning the user id, the secret and the mutable clock the test advances by
// hand.
func enrollTOTP(t *testing.T, svc *Service, st *store.Store) (uuid.UUID, string, *time.Time) {
	t.Helper()
	ctx := context.Background()

	email := uniqueEmail("totp-lockout")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)

	clock := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return clock }

	enrollment, err := svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	if _, err := svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), uuid.Nil); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	return userID, enrollment.Secret, &clock
}

// TestTOTPLockoutAfterRepeatedFailures: consecutive wrong codes — TOTP and
// recovery alike — lock the account, a locked attempt is refused even with a
// correct code, and the lock expires on its own.
func TestTOTPLockoutAfterRepeatedFailures(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	userID, secret, clock := enrollTOTP(t, svc, st)

	// Four wrong TOTP-shaped codes and one wrong recovery-shaped code: the two
	// types share one counter, so the fifth miss arms the lock.
	for i := 0; i < totpMaxFailures-1; i++ {
		if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), "000000"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("wrong TOTP miss %d = %v, want ErrInvalidCode", i, err)
		}
	}
	if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), "ZZZZZZZZZZ"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("wrong recovery miss = %v, want ErrInvalidCode", err)
	}

	// A correct code now buys nothing: the account is locked, and a further
	// attempt stays locked (no reset by repeated probing).
	if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), codeForSecret(t, secret, *clock)); !errors.Is(err, ErrTOTPLocked) {
		t.Fatalf("correct code while locked = %v, want ErrTOTPLocked", err)
	}
	if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), "000000"); !errors.Is(err, ErrTOTPLocked) {
		t.Fatalf("miss while locked = %v, want ErrTOTPLocked", err)
	}

	// Once the window elapses a correct code works again and clears the count.
	*clock = clock.Add(totpLockoutWindow + time.Second)
	if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), codeForSecret(t, secret, *clock)); err != nil {
		t.Fatalf("correct code after the lock expired = %v, want nil", err)
	}
	row, err := svc.store.GetUserTOTP(ctx, pgUUID(userID))
	if err != nil {
		t.Fatalf("GetUserTOTP after success: %v", err)
	}
	if row.FailedAttempts != 0 || row.LockedUntil.Valid {
		t.Fatalf("counters after success = %+v, want 0 and no lock", row)
	}
}

// TestTOTPFailureCounterResetsOnSuccess: a correct code between misses clears
// the count, so four wrong codes either side of a success never lock.
func TestTOTPFailureCounterResetsOnSuccess(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()
	userID, secret, clock := enrollTOTP(t, svc, st)

	for round := 0; round < 2; round++ {
		for i := 0; i < totpMaxFailures-1; i++ {
			if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), "000000"); !errors.Is(err, ErrInvalidCode) {
				t.Fatalf("round %d miss %d = %v, want ErrInvalidCode", round, i, err)
			}
		}
		*clock = clock.Add(totpPeriod)
		if _, err := svc.verifySecondFactor(ctx, pgUUID(userID), codeForSecret(t, secret, *clock)); err != nil {
			t.Fatalf("round %d success = %v, want nil", round, err)
		}
	}
}

// TestTOTPLockoutBlocksEnrollmentCode: the enrollment confirmation counts the
// same misses, so a guesser cannot use the setup step to bypass the lock.
func TestTOTPLockoutBlocksEnrollmentCode(t *testing.T) {
	svc, st := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail("totp-enroll-lockout")
	cleanupUser(t, st, email)
	const password = "s3cret-password"

	registered, err := svc.Register(ctx, email, password, newTestInvite(t, st, email), storeInvites{st}, SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	userID := uuid.MustParse(registered.User.ID)

	clock := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return clock }

	enrollment, err := svc.BeginTOTPEnrollment(ctx, userID)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	for i := 0; i < totpMaxFailures; i++ {
		if _, err := svc.EnableTOTP(ctx, userID, password, "000000", uuid.Nil); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("enable miss %d = %v, want ErrInvalidCode", i, err)
		}
	}
	if _, err := svc.EnableTOTP(ctx, userID, password, codeForSecret(t, enrollment.Secret, clock), uuid.Nil); !errors.Is(err, ErrTOTPLocked) {
		t.Fatalf("enable while locked = %v, want ErrTOTPLocked", err)
	}
}
