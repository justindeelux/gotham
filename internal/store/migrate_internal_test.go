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

// TestMigrateFailsWhenLockHeld is the concurrent case: while another session
// holds the migration advisory lock, a run must fail fast instead of racing it,
// and once the holder goes away the lock must be available again.
func TestMigrateFailsWhenLockHeld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()
	// Returned connections must really close, so ending the holder's session
	// releases its advisory lock instead of parking it back in the pool.
	db.SetMaxIdleConns(0)

	if err := db.PingContext(ctx); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("checkout lock connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&locked); err != nil {
		t.Fatalf("acquire test lock: %v", err)
	}
	if !locked {
		t.Fatal("could not acquire the migration lock for the test")
	}

	if err := Migrate(ctx, dsn, MigrateUp); !errors.Is(err, ErrMigrationInProgress) {
		t.Fatalf("Migrate while locked = %v, want ErrMigrationInProgress", err)
	}

	// Ending the holder's session releases the lock; a fresh run must get it.
	if err := conn.Close(); err != nil {
		t.Fatalf("close lock connection: %v", err)
	}
	if err := Migrate(ctx, dsn, MigrateUp); errors.Is(err, ErrMigrationInProgress) {
		t.Fatal("Migrate after release = ErrMigrationInProgress, want the lock available")
	}
}
