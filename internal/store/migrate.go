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

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	MigrateUp     = "up"
	MigrateDown   = "down"
	MigrateStatus = "status"
)

var ErrUnknownMigrateCommand = errors.New("unknown migration command")

var ErrMigrationInProgress = errors.New("another migration is already in progress")

const migrationLockID int64 = 1026152518

func IsMigrateCommand(command string) bool {
	switch command {
	case MigrateUp, MigrateDown, MigrateStatus:
		return true
	default:
		return false
	}
}

func Migrate(ctx context.Context, dsn, command string) error {
	return migrate(ctx, dsn, command, false)
}

func MigrateFailFast(ctx context.Context, dsn, command string) error {
	return migrate(ctx, dsn, command, true)
}

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
