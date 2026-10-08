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

// TestGitCredentialMigration applies 00041 on a schema migrated through the
// previous tip and asserts the new table: the credential row round-trips,
// the application delete cascades it away, and rolling back drops the table.
func TestGitCredentialMigration(t *testing.T) {
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

	scratch := fmt.Sprintf("gs4_migration_%d", time.Now().UnixNano())
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
	// Migrate through the previous tip (00037 on main; 00038-00040 belong
	// to GS-5/GS-7/GS-9 and land before this file merges).
	if _, err := provider.UpTo(ctx, 40); err != nil {
		t.Fatalf("migrate to 00040: %v", err)
	}
	var tableBefore bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		  WHERE table_name = 'application_git_credentials')`).Scan(&tableBefore); err != nil {
		t.Fatalf("check table before: %v", err)
	}
	if tableBefore {
		t.Fatal("application_git_credentials exists before 00041")
	}

	suffix := time.Now().UnixNano()
	var userID, serverID, envID string
	email := fmt.Sprintf("gs4-mig-%d@example.com", suffix)
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO teams (id, name, is_personal) VALUES ($1, $2, true)`, userID, email); err != nil {
		t.Fatalf("insert personal team: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("gs4-mig-%d", suffix)).Scan(&serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	var projectID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO projects (team_id, name) VALUES ($1, $2) RETURNING id`, userID, "shop").Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO environments (project_id, name) VALUES ($1, $2) RETURNING id`, projectID, "production").Scan(&envID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	var appID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, provider, repo, clone_url, branch, build_pack, source_type)
		 VALUES ($1, $1, $2, $3, 'private-app', '', 'https://git.internal/acme/demo.git', 'https://git.internal/acme/demo.git', 'main', 'dockerfile', 'git_private')
		 RETURNING id`, userID, serverID, envID).Scan(&appID); err != nil {
		t.Fatalf("insert application: %v", err)
	}

	if _, err := provider.UpTo(ctx, 41); err != nil {
		t.Fatalf("migrate to 00041: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO application_git_credentials (application_id, username, ciphertext)
		 VALUES ($1, 'bob', 'sealed-token')`, appID); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	var username, ciphertext string
	if err := db.QueryRowContext(ctx,
		`SELECT username, ciphertext FROM application_git_credentials WHERE application_id = $1`, appID).Scan(&username, &ciphertext); err != nil {
		t.Fatalf("read credential: %v", err)
	}
	if username != "bob" || ciphertext != "sealed-token" {
		t.Errorf("credential = (%q, %q), want (bob, sealed-token)", username, ciphertext)
	}
	// A duplicate insert replaces the row (rotation is an upsert).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO application_git_credentials (application_id, username, ciphertext)
		 VALUES ($1, 'bob', 'rotated') ON CONFLICT (application_id) DO UPDATE SET
		 username = EXCLUDED.username, ciphertext = EXCLUDED.ciphertext, updated_at = now()`, appID); err != nil {
		t.Fatalf("upsert credential: %v", err)
	}

	// Deleting the application cascades the credential row away.
	if _, err := db.ExecContext(ctx, `DELETE FROM applications WHERE id = $1`, appID); err != nil {
		t.Fatalf("delete application: %v", err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM application_git_credentials WHERE application_id = $1`, appID).Scan(&remaining); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d credential rows survived the application delete, want 0", remaining)
	}

	if _, err := provider.DownTo(ctx, 40); err != nil {
		t.Fatalf("roll back to 00040: %v", err)
	}
	var tableAfter bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		  WHERE table_name = 'application_git_credentials')`).Scan(&tableAfter); err != nil {
		t.Fatalf("check table after: %v", err)
	}
	if tableAfter {
		t.Error("rolling back 00041 did not drop application_git_credentials")
	}
}
