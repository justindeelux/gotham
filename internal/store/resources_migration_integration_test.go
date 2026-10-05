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

// TestResourcesEnvironmentMigration applies 00035 on a 00034 schema and
// asserts the environment attachment: environment_id and NOT NULL server_id
// with RESTRICT deletes on all three resource tables, the per-environment
// name indexes (partial for databases and services) and the dropped
// per-creator indexes; then it rolls 00035 back and asserts the revert.
func TestResourcesEnvironmentMigration(t *testing.T) {
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

	scratch := fmt.Sprintf("pe2_migration_%d", time.Now().UnixNano())
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
	if _, err := provider.UpTo(ctx, 34); err != nil {
		t.Fatalf("migrate to 00034: %v", err)
	}

	suffix := time.Now().UnixNano()
	var userID, serverID, projectID, envID pgtype.UUID
	email := fmt.Sprintf("pe2-mig-%d@example.com", suffix)
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO teams (id, name, is_personal) VALUES ($1, $2, true)`, userID, email); err != nil {
		t.Fatalf("insert personal team: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("pe2-mig-%d", suffix)).Scan(&serverID); err != nil {
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

	if _, err := provider.UpTo(ctx, 35); err != nil {
		t.Fatalf("migrate to 00035: %v", err)
	}

	indexExists := func(index string) bool {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`, index).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", index, err)
		}
		return exists
	}
	for _, index := range []string{
		"applications_environment_name_idx",
		"databases_environment_name_idx",
		"services_environment_name_idx",
	} {
		if !indexExists(index) {
			t.Errorf("00035 did not create index %s", index)
		}
	}
	for _, index := range []string{
		"applications_user_name_idx",
		"databases_user_name_idx",
		"services_user_name_idx",
	} {
		if indexExists(index) {
			t.Errorf("00035 did not drop index %s", index)
		}
	}

	// One row per table in the seeded environment and server.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, clone_url, branch, build_pack)
		 VALUES ($1, $1, $2, $3, 'app', 'https://github.com/acme/demo.git', 'main', 'dockerfile')`,
		userID, serverID, envID); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO services (user_id, team_id, server_id, environment_id, name, compose_yaml)
		 VALUES ($1, $1, $2, $3, 'svc', 'services: {}')`,
		userID, serverID, envID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO databases (user_id, team_id, server_id, environment_id, name, engine)
		 VALUES ($1, $1, $2, $3, 'db', 'postgres')`,
		userID, serverID, envID); err != nil {
		t.Fatalf("insert database: %v", err)
	}

	// RESTRICT: neither the environment nor the server can go first.
	if _, err := db.ExecContext(ctx, `DELETE FROM environments WHERE id = $1`, envID); err == nil {
		t.Error("deleting a non-empty environment succeeded, want RESTRICT")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM servers WHERE id = $1`, serverID); err == nil {
		t.Error("deleting a server with resources succeeded, want RESTRICT")
	}
	// Per-environment uniqueness: the same name in another environment fits.
	var stagingID pgtype.UUID
	if err := db.QueryRowContext(ctx,
		`INSERT INTO environments (project_id, name) VALUES ($1, $2) RETURNING id`, projectID, "staging").Scan(&stagingID); err != nil {
		t.Fatalf("insert staging: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, clone_url, branch, build_pack)
		 VALUES ($1, $1, $2, $3, 'app', 'https://github.com/acme/demo.git', 'main', 'dockerfile')`,
		userID, serverID, stagingID); err != nil {
		t.Errorf("same name in another environment: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, clone_url, branch, build_pack)
		 VALUES ($1, $1, $2, $3, 'app', 'https://github.com/acme/demo.git', 'main', 'dockerfile')`,
		userID, serverID, envID); err == nil {
		t.Error("duplicate name in one environment succeeded, want a unique violation")
	}
	// The staging twin would violate the restored per-creator index below,
	// so it goes before the rollback.
	if _, err := db.ExecContext(ctx, `DELETE FROM applications WHERE environment_id = $1`, stagingID); err != nil {
		t.Fatalf("delete staging application: %v", err)
	}

	if _, err := provider.DownTo(ctx, 34); err != nil {
		t.Fatalf("roll back to 00034: %v", err)
	}
	for _, index := range []string{"applications_user_name_idx", "databases_user_name_idx", "services_user_name_idx"} {
		if !indexExists(index) {
			t.Errorf("rolling back 00035 did not restore index %s", index)
		}
	}
}
