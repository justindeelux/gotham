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

// TestToolchainRunningServerUniqueMigrationToleratesDuplicates is the PR #325
// review regression: a database that ran the pre-fix racy claim can already hold
// several running jobs for one node, which would make 00055's CREATE UNIQUE
// INDEX fail and abort the migration. It builds a disposable database, migrates
// it to just before 00055, seeds duplicate running rows, then applies 00055 and
// asserts it succeeds, keeps only the oldest running job per node, and enforces
// the index afterwards.
func TestToolchainRunningServerUniqueMigrationToleratesDuplicates(t *testing.T) {
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

	scratch := fmt.Sprintf("jus176_migration_%d", time.Now().UnixNano())
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

	db, err := sql.Open("pgx", dsnForDatabase(base, scratch))
	if err != nil {
		t.Fatalf("open scratch database: %v", err)
	}
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, store.Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	// Stop at the last migration before the unique running-server index.
	if _, err := provider.UpTo(ctx, 54); err != nil {
		t.Fatalf("migrate to 54: %v", err)
	}

	insertServer := func(name string) string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx,
			`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
			name,
		).Scan(&id); err != nil {
			t.Fatalf("insert server: %v", err)
		}
		return id
	}
	seedRunning := func(serverID, tool string, startedAt time.Time) string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx,
			`INSERT INTO toolchain_jobs (server_id, tool_id, action, status, started_at) VALUES ($1, $2, 'install', 'running', $3) RETURNING id`,
			serverID, tool, startedAt,
		).Scan(&id); err != nil {
			t.Fatalf("insert running job: %v", err)
		}
		return id
	}

	base0 := time.Now().UTC().Truncate(time.Second)
	serverA, serverB := insertServer("jus176-dup-a"), insertServer("jus176-dup-b")
	// Two nodes, each with duplicate running rows; only the oldest per node must
	// survive.
	aOldest := seedRunning(serverA, "a1", base0)
	aNewer := seedRunning(serverA, "a2", base0.Add(time.Minute))
	aNewest := seedRunning(serverA, "a3", base0.Add(2*time.Minute))
	bOldest := seedRunning(serverB, "b1", base0)
	bNewer := seedRunning(serverB, "b2", base0.Add(time.Minute))

	// Apply 00055: this must not fail on the duplicates.
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("apply migration 00055 on duplicate running rows: %v", err)
	}

	status := func(id string) (string, bool) {
		t.Helper()
		var (
			got        string
			finishedAt sql.NullTime
		)
		if err := db.QueryRowContext(ctx,
			`SELECT status, finished_at FROM toolchain_jobs WHERE id = $1`, id,
		).Scan(&got, &finishedAt); err != nil {
			t.Fatalf("read job %s: %v", id, err)
		}
		return got, finishedAt.Valid
	}
	for _, tc := range []struct {
		name       string
		id         string
		wantStatus string
		wantDone   bool
	}{
		{"A oldest survives", aOldest, "running", false},
		{"A newer failed", aNewer, "failed", true},
		{"A newest failed", aNewest, "failed", true},
		{"B oldest survives", bOldest, "running", false},
		{"B newer failed", bNewer, "failed", true},
	} {
		gotStatus, gotDone := status(tc.id)
		if gotStatus != tc.wantStatus || gotDone != tc.wantDone {
			t.Fatalf("%s: status=%s finished=%v, want %s finished=%v", tc.name, gotStatus, gotDone, tc.wantStatus, tc.wantDone)
		}
	}

	var indexCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_indexes WHERE indexname = 'toolchain_jobs_running_server_unique'`,
	).Scan(&indexCount); err != nil {
		t.Fatalf("check index: %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("unique running-server index count = %d, want 1", indexCount)
	}

	// The index now rejects a second running job on a node.
	if _, err := db.ExecContext(ctx, `UPDATE toolchain_jobs SET status = 'running' WHERE id = $1`, aNewer); err == nil {
		t.Fatal("expected a unique violation promoting a second running job on one node")
	}
}
