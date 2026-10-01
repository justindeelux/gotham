package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
)

// defaultTestDSN points at the dev database from deploy/compose.dev.yml. Override
// with GOTHAM_TEST_DSN, e.g. to force a skip in CI with
// GOTHAM_TEST_DSN=postgres://nope.
const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func testDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

// testDSNExplicit reports whether the operator opted in by setting
// GOTHAM_TEST_DSN: an explicit opt-in turns a missing database into a failure
// instead of a skip.
func testDSNExplicit() bool {
	return os.Getenv("GOTHAM_TEST_DSN") != ""
}

// TestStoreUserRoundtrip runs the embedded migrations and verifies an insert and
// fetch roundtrip against a real PostgreSQL server. It skips when no database is
// reachable so CI stays green without one.
func TestStoreUserRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testDSN()

	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}

	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	// Registered before the row cleanup so LIFO order closes the pool last.
	t.Cleanup(pool.Close)

	s := store.New(pool)

	email := fmt.Sprintf("be-0.3-%d@example.com", time.Now().UnixNano())
	created, err := s.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", created.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	fetched, err := s.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("id mismatch: got %v, want %v", fetched.ID, created.ID)
	}
	if fetched.Email != email {
		t.Fatalf("email mismatch: got %q, want %q", fetched.Email, email)
	}
	if !fetched.CreatedAt.Valid || fetched.CreatedAt.Time.IsZero() {
		t.Fatalf("expected created_at to be set, got %+v", fetched.CreatedAt)
	}

	if _, err := s.GetUserByEmail(ctx, "missing-"+email); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows for missing user, got %v", err)
	}
}

// TestStoreCreateFirstUserSerializes proves the first-account guard is atomic:
// concurrent bootstraps with different emails must yield exactly one account.
// It skips unless the users table is empty (the bootstrap precondition), so it
// runs on a fresh database and never touches a populated one.
func TestStoreCreateFirstUserSerializes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := store.Migrate(ctx, testDSN(), store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := store.Open(ctx, testDSN())
	if err != nil {
		if testDSNExplicit() {
			t.Fatalf("open store: %v", err)
		}
		t.Skipf("no database: %v", err)
	}
	defer pool.Close()

	st := store.New(pool)
	count, err := st.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Skipf("users table is not empty (%d rows); the bootstrap guard needs a fresh database", count)
	}

	const attempts = 8
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			email := fmt.Sprintf("first-user-race-%d-%d@example.com", time.Now().UnixNano(), i)
			_, err := st.CreateFirstUser(ctx, email, nil)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	wins := 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, store.ErrInstanceHasAccount):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent bootstraps succeeded %d times, want exactly 1", wins)
	}
}
