package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

// testDSN mirrors the external integration tests: the dev database from
// deploy/compose.dev.yml unless GOTHAM_TEST_DSN overrides it.
func testDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
}

// holdMigrationLock opens a session that holds the migration advisory lock
// until the returned connection is closed. It blocks until the lock is free
// (other packages' Migrate calls also use it) and disables idle connections so
// closing it really ends the session and frees the lock.
func holdMigrationLock(t *testing.T, ctx context.Context, dsn string) *sql.Conn {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxIdleConns(0)
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(ctx); err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("checkout lock connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		t.Fatalf("acquire test lock: %v", err)
	}
	return conn
}

// TestMigrateFailFastFailsWhenLockHeld is the concurrent CLI case: while
// another session holds the migration lock, up and down must both fail fast.
func TestMigrateFailFastFailsWhenLockHeld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testDSN()
	_ = holdMigrationLock(t, ctx, dsn)

	for _, command := range []string{MigrateUp, MigrateDown} {
		t.Run(command, func(t *testing.T) {
			if err := MigrateFailFast(ctx, dsn, command); !errors.Is(err, ErrMigrationInProgress) {
				t.Fatalf("MigrateFailFast(%s) while locked = %v, want ErrMigrationInProgress", command, err)
			}
		})
	}
}

// TestMigrateWaitsForLock is the parallel-test case: Migrate must block on a
// held lock (instead of failing) and proceed once the holder releases it.
func TestMigrateWaitsForLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testDSN()
	conn := holdMigrationLock(t, ctx, dsn)

	done := make(chan error, 1)
	go func() { done <- Migrate(ctx, dsn, MigrateUp) }()

	// The run must still be waiting while the lock is held.
	select {
	case err := <-done:
		t.Fatalf("Migrate returned while the lock was held: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	// Ending the holder's session releases the lock; the waiter must proceed.
	if err := conn.Close(); err != nil {
		t.Fatalf("close lock connection: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Migrate after release: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Migrate did not proceed after the lock was released")
	}
}
