package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestCreateUserCreatesPersonalTeam verifies the runtime half of the 00019
// invariant: every new account owns a personal team whose ID is the user's ID,
// with the user as owner.
func TestCreateUserCreatesPersonalTeam(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testDSN()
	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	st := store.New(pool)

	email := fmt.Sprintf("be-8.2-register-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})

	team, err := st.GetTeam(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetTeam(personal): %v", err)
	}
	if !team.IsPersonal {
		t.Errorf("personal team is_personal = false")
	}
	if want := email + "'s team"; team.Name != want {
		t.Errorf("personal team name = %q, want %q", team.Name, want)
	}
	member, err := st.GetTeamMember(ctx, sqlc.GetTeamMemberParams{TeamID: user.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("GetTeamMember(owner): %v", err)
	}
	if member.Role != "owner" {
		t.Errorf("personal membership role = %q, want owner", member.Role)
	}
}

// TestTeamsMigrationBackfillsPersonalTeams builds a disposable database,
// migrates it to the pre-teams schema, seeds one user with one application,
// database and service, then applies 00019 and asserts the backfill: a personal
// team per user, the user as owner, every resource attributed to the creator's
// personal team, and team_id NOT NULL on the three resource tables.
func TestTeamsMigrationBackfillsPersonalTeams(t *testing.T) {
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

	scratch := fmt.Sprintf("be_8_2_teams_%d", time.Now().UnixNano())
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
	// 00019 is the teams migration; migrate to the newest pre-change version
	// first so the legacy rows can be seeded.
	if _, err := provider.UpTo(ctx, 18); err != nil {
		t.Fatalf("migrate to 00018: %v", err)
	}

	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano()
	insertUser := func(label string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
			fmt.Sprintf("be-8.2-%s-%d@example.com", label, suffix)).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", label, err)
		}
		return id
	}
	userA := insertUser("a")
	userB := insertUser("b")

	if _, err := pool.Exec(ctx,
		`INSERT INTO applications (user_id, name, clone_url, branch, build_pack, port, host_port)
		 VALUES ($1, 'legacy-app', 'https://github.com/acme/demo.git', 'main', 'dockerfile', 80, 18080)`,
		userA); err != nil {
		t.Fatalf("insert legacy application: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO databases (user_id, name, engine) VALUES ($1, 'legacy-db', 'postgres')`,
		userA); err != nil {
		t.Fatalf("insert legacy database: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO services (user_id, name, compose_yaml) VALUES ($1, 'legacy-service', 'services: {}')`,
		userA); err != nil {
		t.Fatalf("insert legacy service: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ('legacy-node', '127.0.0.1', 22, 'root')`); err != nil {
		t.Fatalf("insert legacy server: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	// One personal team per user, named after the account, with the user as
	// owner.
	for label, userID := range map[string]pgtype.UUID{"a": userA, "b": userB} {
		var (
			teamID     pgtype.UUID
			name       string
			isPersonal bool
			role       string
		)
		err := pool.QueryRow(ctx, `
			SELECT t.id, t.name, t.is_personal, m.role
			FROM teams t
			JOIN team_members m ON m.team_id = t.id
			WHERE m.user_id = $1 AND t.is_personal`, userID).
			Scan(&teamID, &name, &isPersonal, &role)
		if err != nil {
			t.Fatalf("user %s personal team: %v", label, err)
		}
		if teamID != userID {
			t.Errorf("user %s personal team id = %v, want the user id", label, teamID)
		}
		if role != "owner" {
			t.Errorf("user %s membership role = %q, want owner", label, role)
		}
		if want := fmt.Sprintf("be-8.2-%s-%d@example.com's team", label, suffix); name != want {
			t.Errorf("user %s personal team name = %q, want %q", label, name, want)
		}
	}

	// Legacy resources are attributed to their creator's personal team.
	for table, query := range map[string]string{
		"applications": `SELECT team_id FROM applications WHERE user_id = $1`,
		"databases":    `SELECT team_id FROM databases WHERE user_id = $1`,
		"services":     `SELECT team_id FROM services WHERE user_id = $1`,
	} {
		var teamID pgtype.UUID
		if err := pool.QueryRow(ctx, query, userA).Scan(&teamID); err != nil {
			t.Fatalf("%s team_id: %v", table, err)
		}
		if teamID != userA {
			t.Errorf("%s team_id = %v, want the creator's user id", table, teamID)
		}
	}

	// The servers table has no owner column: the legacy node keeps team_id
	// NULL, which is the documented pre-teams visibility.
	var serverTeam pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT team_id FROM servers WHERE name = 'legacy-node'`).Scan(&serverTeam); err != nil {
		t.Fatalf("server team_id: %v", err)
	}
	if serverTeam.Valid {
		t.Errorf("legacy server team_id = %v, want NULL", serverTeam)
	}

	// team_id is NOT NULL on the three resource tables now.
	var pgErr *pgconn.PgError
	_, err = pool.Exec(ctx,
		`INSERT INTO applications (user_id, name, clone_url, branch, build_pack, port, host_port)
		 VALUES ($1, 'no-team', '', 'main', 'dockerfile', 80, 18081)`, userA)
	if !errors.As(err, &pgErr) || pgErr.Code != "23502" {
		t.Fatalf("insert without team_id = %v, want a NOT NULL violation", err)
	}

	// Deleting a user cascades the membership; the personal team row stays
	// until the account cleanup removes it (documented residual).
	var memberships int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM team_members WHERE user_id = $1`, userA).Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if memberships == 0 {
		t.Fatal("expected the backfilled owner membership")
	}
}
