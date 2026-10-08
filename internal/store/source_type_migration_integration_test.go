package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
)

// TestApplicationSourceTypeMigration seeds provider-backed and public rows on
// a 00036 schema, applies 00037 and asserts the backfill: github/gitlab rows
// keep the provider-backed flow, everything else is public git, and the
// CHECK rejects unknown types. Then it rolls 00037 back and asserts the
// column is gone.
func TestApplicationSourceTypeMigration(t *testing.T) {
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

	scratch := fmt.Sprintf("gs2_migration_%d", time.Now().UnixNano())
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
	if _, err := provider.UpTo(ctx, 36); err != nil {
		t.Fatalf("migrate to 00036: %v", err)
	}

	suffix := time.Now().UnixNano()
	var userID, serverID, projectID, envID pgtype.UUID
	email := fmt.Sprintf("gs2-mig-%d@example.com", suffix)
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO teams (id, name, is_personal) VALUES ($1, $2, true)`, userID, email); err != nil {
		t.Fatalf("insert personal team: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("gs2-mig-%d", suffix)).Scan(&serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO projects (team_id, name) VALUES ($1, $2) RETURNING id`, userID, "shop").Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO environments (project_id, name) VALUES ($1, $2) RETURNING id`, projectID, "production").Scan(&envID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	seeds := []struct {
		name     string
		provider string
	}{
		{"github-app", "github"},
		{"gitlab-app", "gitlab"},
		{"public-app", "public"},
		{"gitea-app", "gitea"},
		{"legacy-app", ""},
	}
	for _, seed := range seeds {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, provider, repo, clone_url, branch, build_pack)
			 VALUES ($1, $1, $2, $3, $4, $5, 'acme/demo', 'https://github.com/acme/demo.git', 'main', 'dockerfile')`,
			userID, serverID, envID, seed.name, seed.provider); err != nil {
			t.Fatalf("seed %s: %v", seed.name, err)
		}
	}

	if _, err := provider.UpTo(ctx, 37); err != nil {
		t.Fatalf("migrate to 00037: %v", err)
	}

	want := map[string]string{
		"github-app": "github_app",
		"gitlab-app": "gitlab_app",
		"public-app": "git_public",
		"gitea-app":  "git_public",
		"legacy-app": "git_public",
	}
	for name, sourceType := range want {
		var got string
		if err := db.QueryRowContext(ctx,
			`SELECT source_type FROM applications WHERE name = $1`, name).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got != sourceType {
			t.Errorf("%s source_type = %q, want %q", name, got, sourceType)
		}
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, provider, clone_url, source_type)
		 VALUES ($1, $1, $2, $3, 'bad-type', 'public', 'https://github.com/acme/demo.git', 'tarball')`, userID, serverID, envID); err == nil {
		t.Error("unknown source_type inserted, want the CHECK to refuse it")
	}

	if _, err := provider.DownTo(ctx, 36); err != nil {
		t.Fatalf("roll back to 00036: %v", err)
	}
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		  WHERE table_name = 'applications' AND column_name = 'source_type')`).Scan(&exists); err != nil {
		t.Fatalf("check source_type column: %v", err)
	}
	if exists {
		t.Error("rolling back 00037 did not drop the source_type column")
	}
}
