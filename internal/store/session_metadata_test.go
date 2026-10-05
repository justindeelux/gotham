package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// sessionMetadataColumns reports the sessions columns of interest: name to
// "type nullable" (nullable is YES/NO).
func sessionMetadataColumns(t *testing.T, ctx context.Context, conn *pgx.Conn) map[string]string {
	t.Helper()

	rows, err := conn.Query(ctx, `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_name = 'sessions'
		  AND column_name IN ('user_agent', 'ip', 'last_used_at')`)
	if err != nil {
		t.Fatalf("read information_schema: %v", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var name, typ, nullable string
		if err := rows.Scan(&name, &typ, &nullable); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		out[name] = typ + " " + nullable
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read columns: %v", err)
	}
	return out
}

// TestSessionMetadataMigrationUpDownBackfill rolls a scratch database to
// migration 32, seeds a pre-PF-2 session row, and proves 00033 adds the
// columns, backfills last_used_at from created_at, and rolls back cleanly.
func TestSessionMetadataMigrationUpDownBackfill(t *testing.T) {
	base := os.Getenv("GOTHAM_TEST_DSN")
	if base == "" {
		base = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	name := fmt.Sprintf("pf2mig%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		admin, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()

	// Roll to 32 only, then seed a session row with no metadata columns.
	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = sqldb.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if _, err := provider.UpTo(ctx, 32); err != nil {
		t.Fatalf("migrate to 32: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	var userID string
	if err := conn.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id::text`, "pf2-mig@example.com").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var created time.Time
	if err := conn.QueryRow(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, expires_at, credential_version)
		VALUES ($1::uuid, 'prefill-hash', now() + interval '30 days', 1)
		RETURNING created_at`, userID).Scan(&created); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_ = conn.Close(ctx)

	// The product up path applies 00033.
	if err := Migrate(ctx, dsn, MigrateUp); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	conn, err = pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("reconnect scratch database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	cols := sessionMetadataColumns(t, ctx, conn)
	for name, want := range map[string]string{
		"user_agent":   "text YES",
		"ip":           "inet YES",
		"last_used_at": "timestamp with time zone NO",
	} {
		if cols[name] != want {
			t.Errorf("column %s = %q, want %q (all: %v)", name, cols[name], want, cols)
		}
	}
	var backCreated, backUsed time.Time
	var uaStr *string
	var ipStr *string
	if err := conn.QueryRow(ctx, `SELECT created_at, last_used_at, user_agent, ip::text FROM sessions WHERE refresh_hash = 'prefill-hash'`).Scan(&backCreated, &backUsed, &uaStr, &ipStr); err != nil {
		t.Fatalf("read backfilled row: %v", err)
	}
	if !backUsed.Equal(backCreated) {
		t.Errorf("backfilled last_used_at = %v, want created_at %v", backUsed, backCreated)
	}
	if uaStr != nil || ipStr != nil {
		t.Errorf("backfilled metadata = %v/%v, want NULL/NULL", uaStr, ipStr)
	}

	// DownTo(32) rolls 00034 and 00033 back in version order, so a later
	// migration can never strand this test one single-step Down short of
	// the columns again. (00034 only truncates resource tables and drops
	// its own tables on the way down; it leaves sessions alone.)
	if _, err := provider.DownTo(ctx, 32); err != nil {
		t.Fatalf("migrate down to 32: %v", err)
	}
	if cols := sessionMetadataColumns(t, ctx, conn); len(cols) != 0 {
		t.Errorf("columns after down = %v, want none", cols)
	}

	// Up again restores them.
	if err := Migrate(ctx, dsn, MigrateUp); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if cols := sessionMetadataColumns(t, ctx, conn); len(cols) != 3 {
		t.Errorf("columns after re-up = %v, want all three", cols)
	}
}
