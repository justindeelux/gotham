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
func Migrate(ctx context.Context, dsn, command string) error {
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

	switch command {
	case MigrateUp:
		return migrateUp(ctx, provider)
	case MigrateDown:
		return migrateDown(ctx, provider)
	default:
		return migrateStatus(ctx, provider)
	}
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
