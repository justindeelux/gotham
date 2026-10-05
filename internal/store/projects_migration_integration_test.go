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

// TestProjectsEnvironmentsMigration builds a disposable database at the
// pre-change schema (00033), seeds resource rows, applies 00034 and asserts
// the new tables, the case-insensitive indexes and the deliberate resource
// wipe; then it rolls 00034 back and asserts the two tables are gone.
func TestProjectsEnvironmentsMigration(t *testing.T) {
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

	scratch := fmt.Sprintf("pe1_migration_%d", time.Now().UnixNano())
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
	if _, err := provider.UpTo(ctx, 33); err != nil {
		t.Fatalf("migrate to 00033: %v", err)
	}

	suffix := time.Now().UnixNano()
	var userID, serverID pgtype.UUID
	email := fmt.Sprintf("pe1-mig-%d@example.com", suffix)
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// The user is seeded after 00019, so its personal team (team id = user
	// id) is created explicitly; resource rows carry team_id since 00019
	// made it NOT NULL.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO teams (id, name, is_personal) VALUES ($1, $2, true)`, userID, email); err != nil {
		t.Fatalf("insert personal team: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $1, 'owner')`, userID); err != nil {
		t.Fatalf("insert owner membership: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("pe1-mig-%d", suffix)).Scan(&serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	seedResource := func(table, name string) {
		t.Helper()
		switch table {
		case "applications":
			_, err := db.ExecContext(ctx,
				`INSERT INTO applications (user_id, team_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
				 VALUES ($1, $1, $2, $3, 'https://github.com/acme/demo.git', 'main', 'dockerfile', $4, 80, 18080)`,
				userID, serverID, name, fmt.Sprintf("%s.example.com", name))
			if err != nil {
				t.Fatalf("insert application: %v", err)
			}
			if _, err := db.ExecContext(ctx,
				`INSERT INTO deployments (application_id) VALUES ((SELECT id FROM applications WHERE name = $1))`, name); err != nil {
				t.Fatalf("insert deployment: %v", err)
			}
		case "services":
			_, err := db.ExecContext(ctx,
				`INSERT INTO services (user_id, team_id, server_id, name, compose_yaml) VALUES ($1, $1, $2, $3, 'services: {}')`,
				userID, serverID, name)
			if err != nil {
				t.Fatalf("insert service: %v", err)
			}
		case "databases":
			_, err := db.ExecContext(ctx,
				`INSERT INTO databases (user_id, team_id, server_id, name, engine, version) VALUES ($1, $1, $2, $3, 'postgres', '16')`,
				userID, serverID, name)
			if err != nil {
				t.Fatalf("insert database: %v", err)
			}
		}
	}
	seedResource("applications", fmt.Sprintf("pe1-app-%d", suffix))
	seedResource("services", fmt.Sprintf("pe1-svc-%d", suffix))
	seedResource("databases", fmt.Sprintf("pe1-db-%d", suffix))

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	tableExists := func(table string) bool {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		return exists
	}
	if !tableExists("projects") || !tableExists("environments") {
		t.Fatal("00034 did not create projects and environments")
	}
	for _, index := range []string{"projects_team_name_lower_idx", "environments_project_name_lower_idx"} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`, index).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", index, err)
		}
		if !exists {
			t.Fatalf("index %s was not created", index)
		}
	}
	// The deliberate wipe: every resource table is empty.
	for _, table := range []string{"applications", "databases", "services"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s has %d rows, want the deliberate wipe to empty it", table, count)
		}
	}
	// Users, teams and servers survive the wipe.
	var users int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users == 0 {
		t.Fatal("the wipe took users with it")
	}

	// Down rolls 00034 back: the two tables are gone.
	if _, err := provider.Down(ctx); err != nil {
		t.Fatalf("roll back 00034: %v", err)
	}
	if tableExists("projects") || tableExists("environments") {
		t.Fatal("Down did not drop projects and environments")
	}
}
