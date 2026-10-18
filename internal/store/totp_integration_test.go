package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
)

// TestUserTOTPGuardsAreAtomic exercises the two trust boundaries that keep a
// second factor single use: a recovery code is consumed exactly once, and a
// code step is advanced exactly once, even under concurrent requests.
func TestUserTOTPGuardsAreAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	s := store.New(pool)

	email := fmt.Sprintf("totp-guard-%d@example.com", time.Now().UnixNano())
	user, err := s.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.DB.Exec(cleanup, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	const startStep = int64(100)
	if stored, err := s.UpsertUserTOTPSecret(ctx, user.ID, "sealed-secret"); err != nil || !stored {
		t.Fatalf("UpsertUserTOTPSecret = (%v, %v), want (true, nil)", stored, err)
	}
	enabled, err := s.EnableUserTOTP(ctx, user.ID, startStep, []string{"hash-one", "hash-two"})
	if err != nil {
		t.Fatalf("EnableUserTOTP: %v", err)
	}
	if !enabled {
		t.Fatalf("EnableUserTOTP did not enable the row")
	}
	// A second enable on an already-enabled row is refused (zero rows).
	if enabled, err := s.EnableUserTOTP(ctx, user.ID, startStep, []string{"hash-three"}); err != nil || enabled {
		t.Fatalf("second EnableUserTOTP = (%v, %v), want (false, nil)", enabled, err)
	}

	// Concurrent consumption of the same recovery code: exactly one wins.
	if got := concurrentBool(t, 8, func() (bool, error) {
		return s.ConsumeRecoveryCode(ctx, user.ID, "hash-one")
	}); got != 1 {
		t.Fatalf("concurrent ConsumeRecoveryCode winners = %d, want 1", got)
	}
	// The consumed code is gone; the sibling is still usable.
	if consumed, err := s.ConsumeRecoveryCode(ctx, user.ID, "hash-one"); err != nil || consumed {
		t.Fatalf("re-consumed code = (%v, %v), want (false, nil)", consumed, err)
	}
	if consumed, err := s.ConsumeRecoveryCode(ctx, user.ID, "hash-two"); err != nil || !consumed {
		t.Fatalf("sibling code = (%v, %v), want (true, nil)", consumed, err)
	}

	// Concurrent advance to the same step: exactly one wins, and an older step
	// is always refused.
	if got := concurrentBool(t, 8, func() (bool, error) {
		return s.AdvanceUserTOTPStep(ctx, user.ID, startStep+5)
	}); got != 1 {
		t.Fatalf("concurrent AdvanceUserTOTPStep winners = %d, want 1", got)
	}
	if advanced, err := s.AdvanceUserTOTPStep(ctx, user.ID, startStep+4); err != nil || advanced {
		t.Fatalf("older step = (%v, %v), want (false, nil)", advanced, err)
	}
	if advanced, err := s.AdvanceUserTOTPStep(ctx, user.ID, startStep+6); err != nil || !advanced {
		t.Fatalf("newer step = (%v, %v), want (true, nil)", advanced, err)
	}
}

// concurrentBool runs fn from function concurrent goroutines and reports how
// many returned true without an error.
func concurrentBool(t *testing.T, workers int, fn func() (bool, error)) int {
	t.Helper()

	var (
		mu      sync.Mutex
		winners int
		once    sync.Once
		failure error
		wg      sync.WaitGroup
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := fn()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				once.Do(func() { failure = err })
				return
			}
			if ok {
				winners++
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		t.Fatalf("concurrent call failed: %v", failure)
	}
	return winners
}

// TestUserTOTPAttemptBurstIsBounded: attempts are counted atomically and
// refused once the threshold is reached, so a parallel burst can never overshoot
// the threshold or slip past an armed lock. Clearing resets both.
func TestUserTOTPAttemptBurstIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	s := store.New(pool)

	email := fmt.Sprintf("totp-lockout-%d@example.com", time.Now().UnixNano())
	user, err := s.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.DB.Exec(cleanup, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})
	if stored, err := s.UpsertUserTOTPSecret(ctx, user.ID, "sealed-secret"); err != nil || !stored {
		t.Fatalf("UpsertUserTOTPSecret = (%v, %v), want (true, nil)", stored, err)
	}
	if _, err := s.EnableUserTOTP(ctx, user.ID, 100, []string{"hash-one"}); err != nil {
		t.Fatalf("EnableUserTOTP: %v", err)
	}

	const threshold = 5
	now := time.Now()
	deadline := now.Add(5 * time.Minute)
	for i := 0; i < threshold-1; i++ {
		if reserved, err := s.BeginUserTOTPAttempt(ctx, user.ID, threshold, now, deadline); err != nil || !reserved {
			t.Fatalf("attempt %d = (%v, %v), want (true, nil)", i, reserved, err)
		}
	}
	row, err := s.GetUserTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserTOTP: %v", err)
	}
	if row.FailedAttempts != threshold-1 || row.LockedUntil.Valid {
		t.Fatalf("below threshold = %+v, want %d attempts and no lock", row, threshold-1)
	}

	// Twenty concurrent attempts: only the last free slot is reserved, the rest
	// are refused because the lock is armed, and the counter stops at threshold.
	if reserved := concurrentBool(t, 20, func() (bool, error) {
		return s.BeginUserTOTPAttempt(ctx, user.ID, threshold, now, deadline)
	}); reserved != 1 {
		t.Fatalf("concurrent attempts reserved = %d, want 1 (the last free slot)", reserved)
	}
	row, err = s.GetUserTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserTOTP after burst: %v", err)
	}
	if row.FailedAttempts != threshold || !row.LockedUntil.Valid {
		t.Fatalf("locked row = %+v, want %d attempts and a lock", row, threshold)
	}

	// While locked, an attempt is refused and counted no further.
	if reserved, err := s.BeginUserTOTPAttempt(ctx, user.ID, threshold, now, deadline); err != nil || reserved {
		t.Fatalf("attempt while locked = (%v, %v), want (false, nil)", reserved, err)
	}
	row, err = s.GetUserTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserTOTP while locked: %v", err)
	}
	if row.FailedAttempts != threshold {
		t.Fatalf("counter after a locked attempt = %d, want %d (no overshoot)", row.FailedAttempts, threshold)
	}

	if err := s.ClearUserTOTPFailures(ctx, user.ID); err != nil {
		t.Fatalf("ClearUserTOTPFailures: %v", err)
	}
	row, err = s.GetUserTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserTOTP after clear: %v", err)
	}
	if row.FailedAttempts != 0 || row.LockedUntil.Valid {
		t.Fatalf("cleared row = %+v, want 0 attempts and no lock", row)
	}
}

// TestUserTOTPUpsertResetsLockAndGuardsLiveSecret covers the enroll fixes
// (JUS-198): re-enrolling a pending secret clears any stale lock state, and the
// upsert can never overwrite a live (enabled) secret, so a re-enroll that loses
// the race is refused instead of silently swapping the active factor.
func TestUserTOTPUpsertResetsLockAndGuardsLiveSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	s := store.New(pool)

	email := fmt.Sprintf("totp-enroll-guard-%d@example.com", time.Now().UnixNano())
	user, err := s.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.DB.Exec(cleanup, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	// A pending enrollment whose lockout has been armed by failed confirmations.
	if stored, err := s.UpsertUserTOTPSecret(ctx, user.ID, "pending-secret"); err != nil || !stored {
		t.Fatalf("UpsertUserTOTPSecret = (%v, %v), want (true, nil)", stored, err)
	}
	now := time.Now()
	deadline := now.Add(5 * time.Minute)
	for i := 0; i < 5; i++ {
		if _, err := s.BeginUserTOTPAttempt(ctx, user.ID, 5, now, deadline); err != nil {
			t.Fatalf("BeginUserTOTPAttempt: %v", err)
		}
	}

	// Re-enrolling replaces the pending secret and drops the stale lock state.
	if stored, err := s.UpsertUserTOTPSecret(ctx, user.ID, "fresh-secret"); err != nil || !stored {
		t.Fatalf("re-enroll = (%v, %v), want (true, nil)", stored, err)
	}
	row, err := s.GetUserTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserTOTP: %v", err)
	}
	if row.SecretEnc != "fresh-secret" || row.FailedAttempts != 0 || row.LockedUntil.Valid {
		t.Fatalf("re-enrolled row = %+v, want the fresh secret and no lock state", row)
	}

	// Once enabled, the pending upsert is refused and leaves the live secret.
	if enabled, err := s.EnableUserTOTP(ctx, user.ID, 100, []string{"hash-one"}); err != nil || !enabled {
		t.Fatalf("EnableUserTOTP = (%v, %v), want (true, nil)", enabled, err)
	}
	if stored, err := s.UpsertUserTOTPSecret(ctx, user.ID, "replacement"); err != nil || stored {
		t.Fatalf("overwrite of a live secret = (%v, %v), want (false, nil)", stored, err)
	}
	row, err = s.GetUserTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserTOTP after overwrite: %v", err)
	}
	if row.SecretEnc != "fresh-secret" || !row.Enabled {
		t.Fatalf("live row = %+v, want the original secret still enabled", row)
	}
}

// TestUserTOTPRevocationJoinsTransaction proves the session revoke shares the
// caller's transaction (JUS-198): a disable's row delete and the other-session
// revoke commit together, and a later failure in the same operation rolls back
// both, so a revocation failure can never leave old sessions alive behind a
// completed change.
func TestUserTOTPRevocationJoinsTransaction(t *testing.T) {
	st, ctx := openVersionTestStore(t)
	user, _ := versionTestUser(t, ctx, st, "totp-tx")
	current := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)
	other := versionTestSession(t, ctx, st, user.ID, user.CredentialVersion)

	if stored, err := st.UpsertUserTOTPSecret(ctx, user.ID, "sealed-secret"); err != nil || !stored {
		t.Fatalf("UpsertUserTOTPSecret = (%v, %v), want (true, nil)", stored, err)
	}
	if enabled, err := st.EnableUserTOTP(ctx, user.ID, 100, []string{"hash-one"}); err != nil || !enabled {
		t.Fatalf("EnableUserTOTP = (%v, %v), want (true, nil)", enabled, err)
	}

	// Model a disable: drop the TOTP row and revoke the other sessions in one
	// transaction, then fail before the commit.
	boom := errors.New("later step failed")
	err := st.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteUserTOTP(ctx, user.ID); err != nil {
			return err
		}
		if _, _, err := tx.RevokeOtherSessionsGuarded(ctx, user.ID, current.ID); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx error = %v, want the injected failure", err)
	}

	// Rolled back: the TOTP row and the other session both survive.
	if _, err := st.GetUserTOTP(ctx, user.ID); err != nil {
		t.Fatalf("the TOTP row did not roll back with the revocation: %v", err)
	}
	if _, err := st.GetSessionByRefreshHash(ctx, other.RefreshHash); err != nil {
		t.Fatalf("the other session did not roll back: %v", err)
	}

	// A committed transaction does both at once.
	if err := st.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteUserTOTP(ctx, user.ID); err != nil {
			return err
		}
		_, _, err := tx.RevokeOtherSessionsGuarded(ctx, user.ID, current.ID)
		return err
	}); err != nil {
		t.Fatalf("InTx commit: %v", err)
	}
	if _, err := st.GetUserTOTP(ctx, user.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the TOTP row survived the committed disable: %v", err)
	}
	if _, err := st.GetSessionByRefreshHash(ctx, other.RefreshHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the other session survived the committed revoke: %v", err)
	}
	if _, err := st.GetSessionByRefreshHash(ctx, current.RefreshHash); err != nil {
		t.Fatalf("the caller's own session did not survive: %v", err)
	}
}
