package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/pressly/goose/v3"

	// The pgx database/sql driver registers itself as "pgx", which goose uses.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Migrate subcommands. "up" applies every pending migration, "down" rolls back
// only the most recently applied migration, and "status" reports state without
// changing the schema.
const (
	MigrateUp     = "up"
	MigrateDown   = "down"
	MigrateStatus = "status"
)

// ErrUnknownMigrateCommand is returned by Migrate for an unsupported subcommand.
var ErrUnknownMigrateCommand = errors.New("unknown migration command")

// ErrMigrationInProgress is returned when another `gotham migrate` holds the
// migration lock. The schema is shared, so the second run fails instead of
// interleaving goose's bookkeeping with the first.
var ErrMigrationInProgress = errors.New("another migration is already in progress")

// migrationLockID is the session-level advisory lock that serializes migration
// runs across processes. It is the CRC-32 of "gotham-migrate", kept distinct
// from goose's own lock ID so an unrelated goose locker never collides.
const migrationLockID int64 = 1026152518

// IsMigrateCommand reports whether command is a supported migrate subcommand.
func IsMigrateCommand(command string) bool {
	switch command {
	case MigrateUp, MigrateDown, MigrateStatus:
		return true
	default:
		return false
	}
}

// Migrate runs the embedded migrations against dsn. The schema is forward-only:
// "down" exists for local development and rolls back a single version.
//
// "up" and "down" mutate the shared schema, so they hold a session-level
// PostgreSQL advisory lock for the whole run and wait for any concurrent run to
// finish, respecting ctx. The lock is released when the connection closes
// (including on a crash). "status" only reads, so it runs without the lock.
//
// Callers that must not wait use MigrateFailFast.
func Migrate(ctx context.Context, dsn, command string) error {
	return migrate(ctx, dsn, command, false)
}

// MigrateFailFast is Migrate without the wait: when another run holds the
// migration lock it returns ErrMigrationInProgress immediately. The CLI uses it
// so a second `gotham migrate` fails clearly instead of queueing behind the
// first. Test setup uses Migrate so parallel packages serialize on the lock.
func MigrateFailFast(ctx context.Context, dsn, command string) error {
	return migrate(ctx, dsn, command, true)
}

// migrate is the shared implementation behind Migrate and MigrateFailFast.
func migrate(ctx context.Context, dsn, command string, failFast bool) error {
	if !IsMigrateCommand(command) {
		return fmt.Errorf("%w: %q", ErrUnknownMigrateCommand, command)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, Migrations())
	if err != nil {
		return fmt.Errorf("init migrations: %w", err)
	}

	if command == MigrateStatus {
		return migrateStatus(ctx, provider)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration lock connection: %w", err)
	}
	// Closing the connection ends the session and releases the advisory lock;
	// no explicit unlock is needed, so a killed process cannot leave it held.
	defer func() { _ = conn.Close() }()

	if failFast {
		var locked bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&locked); err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if !locked {
			return ErrMigrationInProgress
		}
	} else if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}

	if command == MigrateUp {
		return migrateUp(ctx, provider)
	}
	return migrateDown(ctx, provider)
}

// migrateUp applies every pending migration and reports what ran.
func migrateUp(ctx context.Context, provider *goose.Provider) error {
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply pending migrations: %w", err)
	}
	if len(results) == 0 {
		fmt.Println("no pending migrations")
		return nil
	}
	for _, result := range results {
		fmt.Println(result.String())
	}
	return nil
}

// migrateDown rolls back the most recently applied migration, printing it first
// so the operator can see what is about to be reverted.
func migrateDown(ctx context.Context, provider *goose.Provider) error {
	target, err := lastApplied(ctx, provider)
	if err != nil {
		return err
	}
	if target == nil {
		fmt.Println("no applied migrations to roll back")
		return nil
	}
	fmt.Printf("rolling back %d %s\n", target.Source.Version, filepath.Base(target.Source.Path))

	result, err := provider.Down(ctx)
	if err != nil {
		return fmt.Errorf("roll back latest migration: %w", err)
	}
	fmt.Println(result.String())
	return nil
}

// migrateStatus prints one row per known migration.
func migrateStatus(ctx context.Context, provider *goose.Provider) error {
	statuses, err := provider.Status(ctx)
	if err != nil {
		return fmt.Errorf("read migration status: %w", err)
	}
	if len(statuses) == 0 {
		fmt.Println("no migrations found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tSTATE\tAPPLIED AT\tPATH")
	for _, status := range statuses {
		appliedAt := "-"
		if !status.AppliedAt.IsZero() {
			appliedAt = status.AppliedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			status.Source.Version,
			status.State,
			appliedAt,
			filepath.Base(status.Source.Path),
		)
	}
	return w.Flush()
}

// lastApplied returns the highest applied migration, or nil when none ran.
func lastApplied(ctx context.Context, provider *goose.Provider) (*goose.MigrationStatus, error) {
	statuses, err := provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("read migration status: %w", err)
	}
	var last *goose.MigrationStatus
	for _, status := range statuses {
		if status.State == goose.StateApplied {
			last = status
		}
	}
	return last, nil
}
