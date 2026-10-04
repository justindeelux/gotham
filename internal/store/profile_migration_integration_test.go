package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
)

// TestProfileDisplayNameMigration builds a disposable database at the
// pre-profile schema, seeds a user, then rolls 00032 up and down: up adds a
// nullable display_name that accepts a value and NULL but rejects the empty
// string and overlong names; down drops the column again.
func TestProfileDisplayNameMigration(t *testing.T) {
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

	scratch := fmt.Sprintf("pf1_profile_%d", time.Now().UnixNano())
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
	// 00032 is the profile migration; stop at the newest pre-change version.
	if _, err := provider.UpTo(ctx, 31); err != nil {
		t.Fatalf("migrate to 00031: %v", err)
	}

	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer pool.Close()

	var userID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("pf1-mig-%d@example.com", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	var name *string
	if err := pool.QueryRow(ctx, `UPDATE users SET display_name = $2 WHERE id = $1 RETURNING display_name`,
		userID, "Ada").Scan(&name); err != nil {
		t.Fatalf("set display name: %v", err)
	}
	if name == nil || *name != "Ada" {
		t.Fatalf("display name = %v, want Ada", name)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET display_name = NULL WHERE id = $1`, userID); err != nil {
		t.Fatalf("clear display name: %v", err)
	}

	var pgErr *pgconn.PgError
	for value, want := range map[string]string{"": "23514", strings.Repeat("x", 65): "23514"} {
		_, err := pool.Exec(ctx, `UPDATE users SET display_name = $2 WHERE id = $1`, userID, value)
		if !errors.As(err, &pgErr) || pgErr.Code != want {
			t.Fatalf("display name %q err = %v, want check violation %s", value, err, want)
		}
	}

	if _, err := provider.DownTo(ctx, 31); err != nil {
		t.Fatalf("migrate down to 00031: %v", err)
	}
	var columnExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'display_name')`).Scan(&columnExists); err != nil {
		t.Fatalf("check column: %v", err)
	}
	if columnExists {
		t.Fatal("display_name still exists after the down migration")
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate back up: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'display_name')`).Scan(&columnExists); err != nil {
		t.Fatalf("recheck column: %v", err)
	}
	if !columnExists {
		t.Fatal("display_name missing after re-applying the migration")
	}
}
