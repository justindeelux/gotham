package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
)

// TestSharedVariablesMigration applies 00036 on a scratch database and asserts
// the table, the scope uniqueness index and the two cascades; then it rolls
// 00036 back and asserts the table is gone.
func TestSharedVariablesMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base := testDSN()
	admin, err := sql.Open("pgx", dsnForDatabase(base, "postgres"))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	scratch := fmt.Sprintf("pe3_migration_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+scratch); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but a disposable database cannot be created: %v", err)
		}
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)"); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	scratchDSN := dsnForDatabase(base, scratch)
	db, err := sql.Open("pgx", scratchDSN)
	if err != nil {
		t.Fatalf("open scratch database: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, store.Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if _, err := provider.UpTo(ctx, 35); err != nil {
		t.Fatalf("migrate to 00035: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'shared_variables')`).Scan(&exists); err != nil {
		t.Fatalf("check table: %v", err)
	}
	if !exists {
		t.Fatal("00036 did not create shared_variables")
	}
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'shared_variables_scope_key_idx')`).Scan(&exists); err != nil {
		t.Fatalf("check index: %v", err)
	}
	if !exists {
		t.Fatal("00036 did not create the scope uniqueness index")
	}

	// Both cascades: deleting a project removes its project rows, deleting an
	// environment removes its rows.
	suffix := time.Now().UnixNano()
	var userID, projectID, envID string
	email := fmt.Sprintf("pe3-mig-%d@example.com", suffix)
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO teams (id, name, is_personal) VALUES ($1, $2, true)`, userID, email); err != nil {
		t.Fatalf("insert personal team: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO projects (team_id, name) VALUES ($1, 'shop') RETURNING id`, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO environments (project_id, name) VALUES ($1, 'staging') RETURNING id`, projectID).Scan(&envID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_variables (project_id, key, value) VALUES ($1, 'A', '1')`, projectID); err != nil {
		t.Fatalf("insert project variable: %v", err)
	}
	// The project level (NULL environment) and one environment row may share
	// a key; two rows in the same scope may not.
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_variables (project_id, environment_id, key, value) VALUES ($1, $2, 'A', '2')`, projectID, envID); err != nil {
		t.Fatalf("insert environment variable: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_variables (project_id, key, value) VALUES ($1, 'A', '3')`, projectID); err == nil {
		t.Fatal("duplicate project-level key inserted, want a uniqueness refusal")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM environments WHERE id = $1`, envID); err != nil {
		t.Fatalf("delete environment: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM shared_variables`).Scan(&count); err != nil {
		t.Fatalf("count after environment delete: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d rows survive the environment delete, want 1 (the project row)", count)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM shared_variables`).Scan(&count); err != nil {
		t.Fatalf("count after project delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d rows survive the project delete, want 0", count)
	}

	if _, err := provider.DownTo(ctx, 35); err != nil {
		t.Fatalf("roll back to 00035: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'shared_variables')`).Scan(&exists); err != nil {
		t.Fatalf("check table after down: %v", err)
	}
	if exists {
		t.Fatal("Down did not drop shared_variables")
	}
}
