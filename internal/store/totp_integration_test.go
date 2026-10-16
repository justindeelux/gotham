package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

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
	if err := s.UpsertUserTOTPSecret(ctx, user.ID, "sealed-secret"); err != nil {
		t.Fatalf("UpsertUserTOTPSecret: %v", err)
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
