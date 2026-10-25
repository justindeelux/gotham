package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

func removeTeamsScratch(t *testing.T) (context.Context, string, *goose.Provider) {
	t.Helper()

	base := os.Getenv("GOTHAM_TEST_DSN")
	if base == "" {
		base = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	name := fmt.Sprintf("rmteam%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		if _, err := conn.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()

	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	return ctx, dsn, provider
}

func TestRemoveTeamsMigrationRenamesSeededDuplicates(t *testing.T) {
	ctx, dsn, provider := removeTeamsScratch(t)

	if _, err := provider.UpTo(ctx, 63); err != nil {
		t.Fatalf("migrate to 63: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	suffix := time.Now().UnixNano()
	seedUser := func(email string) string {
		t.Helper()
		var id string
		if err := conn.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id::text`, email).Scan(&id); err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
		return id
	}
	seedUser(fmt.Sprintf("rm-team-a-%d@example.com", suffix))
	userB := seedUser(fmt.Sprintf("rm-team-b-%d@example.com", suffix))

	teamA, teamB := uuid.NewString(), uuid.NewString()
	if _, err := conn.Exec(ctx, `INSERT INTO teams (id, name) VALUES ($1::uuid, 'a'), ($2::uuid, 'b')`, teamA, teamB); err != nil {
		t.Fatalf("seed teams: %v", err)
	}
	var serverID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user, team_id) VALUES ('rm-node', '127.0.0.1', 22, 'root', $1::uuid) RETURNING id::text`,
		teamA).Scan(&serverID); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	insertProject := func(teamID, name, createdAt string) (projectID, environmentID string) {
		t.Helper()
		if err := conn.QueryRow(ctx,
			`INSERT INTO projects (team_id, name, created_at) VALUES ($1::uuid, $2, $3) RETURNING id::text`,
			teamID, name, createdAt).Scan(&projectID); err != nil {
			t.Fatalf("seed project %s: %v", name, err)
		}
		if err := conn.QueryRow(ctx,
			`INSERT INTO environments (project_id, name) VALUES ($1::uuid, 'production') RETURNING id::text`,
			projectID).Scan(&environmentID); err != nil {
			t.Fatalf("seed environment %s: %v", name, err)
		}
		return projectID, environmentID
	}
	firstProject, _ := insertProject(teamA, "Demo", "2026-01-01T00:00:00Z")
	secondProject, secondEnv := insertProject(teamB, "demo", "2026-01-02T00:00:00Z")

	if _, err := conn.Exec(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, provider, repo, clone_url, branch, build_pack, source_type)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'collision-app', 'github', 'acme/demo', 'https://github.com/acme/demo.git', 'main', 'dockerfile', 'git_public')`,
		userB, teamB, serverID, secondEnv); err != nil {
		t.Fatalf("seed application: %v", err)
	}
	_ = conn.Close(ctx)

	if err := Migrate(ctx, dsn, MigrateUp); err != nil {
		t.Fatalf("migrate up with duplicates: %v", err)
	}

	conn, err = pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("reconnect scratch database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var firstName, secondName string
	if err := conn.QueryRow(ctx, `SELECT name FROM projects WHERE id=$1::uuid`, firstProject).Scan(&firstName); err != nil {
		t.Fatalf("read first project: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT name FROM projects WHERE id=$1::uuid`, secondProject).Scan(&secondName); err != nil {
		t.Fatalf("read second project: %v", err)
	}
	if firstName != "Demo" {
		t.Fatalf("earliest project renamed to %q, want Demo", firstName)
	}
	if !strings.HasPrefix(secondName, "demo (") || !strings.HasSuffix(secondName, ")") || secondName == "demo" {
		t.Fatalf("later duplicate name = %q, want a deterministic rename", secondName)
	}
	var projects, apps int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&projects); err != nil || projects != 2 {
		t.Fatalf("projects after migration = %d, %v; want both preserved", projects, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM applications WHERE name='collision-app'`).Scan(&apps); err != nil || apps != 1 {
		t.Fatalf("application after migration = %d, %v; want preserved", apps, err)
	}
	assertRemoveTeamsSchema(t, ctx, conn)
}

func TestRemoveTeamsMigrationEmptyDatabaseAndIrreversibleDown(t *testing.T) {
	ctx, dsn, provider := removeTeamsScratch(t)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up from empty: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	assertRemoveTeamsSchema(t, ctx, conn)
	_ = conn.Close(ctx)

	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("down of migration 64 succeeded, want a loud refusal")
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil || version != 64 {
		t.Fatalf("version after refused down = %d, %v; want 64", version, err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up after refused down: %v", err)
	}
}

func assertRemoveTeamsSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for _, table := range []string{"teams", "team_members", "invites"} {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if exists {
			t.Errorf("table %s still exists", table)
		}
	}
	rows, err := conn.Query(ctx, `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE column_name = 'team_id' AND table_schema = 'public'`)
	if err != nil {
		t.Fatalf("read team_id columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatalf("scan team_id column: %v", err)
		}
		t.Errorf("%s.%s still exists", table, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read team_id columns: %v", err)
	}
	var indexed bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('projects_name_lower_idx') IS NOT NULL`).Scan(&indexed); err != nil || !indexed {
		t.Fatalf("projects instance-wide index = %v, %v", indexed, err)
	}
	var distinct int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM (SELECT lower(name) FROM projects GROUP BY 1 HAVING count(*) > 1) dup`).Scan(&distinct); err != nil || distinct != 0 {
		t.Fatalf("duplicate project names after migration = %d, %v", distinct, err)
	}
}
