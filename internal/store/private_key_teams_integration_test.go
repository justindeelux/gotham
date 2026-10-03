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

// TestPrivateKeyTeamsBackfill builds a disposable database, migrates it to the
// pre-key-teams schema, seeds legacy keys in every attribution case, then
// applies 00030 and asserts the backfill: a key referenced only by one team's
// servers and a deploy-key row follow that team, while a key referenced by two
// teams, by a legacy (team_id NULL) server, or by nothing keeps team_id NULL
// (usable only without a team scope or by a referencing team; see
// ServerService.keyForTeam).
func TestPrivateKeyTeamsBackfill(t *testing.T) {
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

	scratch := fmt.Sprintf("be_30_keyteams_%d", time.Now().UnixNano())
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
	// 00030 is the key-teams migration; migrate to the newest pre-change
	// version first so the legacy rows can be seeded.
	if _, err := provider.UpTo(ctx, 29); err != nil {
		t.Fatalf("migrate to 00029: %v", err)
	}

	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano()
	makeTeam := func(tag string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO teams (name) VALUES ($1) RETURNING id`,
			fmt.Sprintf("key-team-%s-%d", tag, suffix)).Scan(&id); err != nil {
			t.Fatalf("insert team: %v", err)
		}
		return id
	}
	teamA := makeTeam("a")
	teamB := makeTeam("b")

	var owner pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`,
		fmt.Sprintf("be-30-%d@example.com", suffix)).Scan(&owner); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var appTeam pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO applications (user_id, team_id, name, port, host_port)
		 VALUES ($1, $2, 'legacy-app', 80, 18080) RETURNING team_id`,
		owner, teamA).Scan(&appTeam); err != nil {
		t.Fatalf("insert legacy application: %v", err)
	}

	insertKey := func(name string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO private_keys (name, encrypted_key) VALUES ($1, 'sealed') RETURNING id`,
			name).Scan(&id); err != nil {
			t.Fatalf("insert key %s: %v", name, err)
		}
		return id
	}
	single := insertKey("k-single")
	multi := insertKey("k-multi")
	insertKey("k-none")
	legacyOnly := insertKey("k-legacy")
	shared := insertKey("k-shared")
	deploy := insertKey("k-deploy")

	insertServer := func(name string, keyID, teamID pgtype.UUID) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO servers (name, ip, port, ssh_user, ssh_key_id, team_id)
			 VALUES ($1, '127.0.0.1', 22, 'root', $2, $3)`,
			name, keyID, teamID); err != nil {
			t.Fatalf("insert server %s: %v", name, err)
		}
	}
	insertServer("s-single", single, teamA)
	insertServer("s-multi-a", multi, teamA)
	insertServer("s-multi-b", multi, teamB)
	insertServer("s-legacy", legacyOnly, pgtype.UUID{})
	insertServer("s-shared-a", shared, teamA)
	insertServer("s-shared-legacy", shared, pgtype.UUID{})

	if _, err := pool.Exec(ctx,
		`INSERT INTO application_deploy_keys (application_id, private_key_id) VALUES (
			(SELECT id FROM applications WHERE name = 'legacy-app'), $1)`,
		deploy); err != nil {
		t.Fatalf("insert legacy deploy-key mapping: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	keyTeam := func(name string) pgtype.UUID {
		t.Helper()
		var teamID pgtype.UUID
		if err := pool.QueryRow(ctx, `SELECT team_id FROM private_keys WHERE name = $1`, name).Scan(&teamID); err != nil {
			t.Fatalf("key %s team_id: %v", name, err)
		}
		return teamID
	}
	if got := keyTeam("k-single"); got != teamA {
		t.Errorf("k-single team = %v, want the referencing team %v", got, teamA)
	}
	if got := keyTeam("k-deploy"); got != teamA {
		t.Errorf("k-deploy team = %v, want the owning app's team %v", got, teamA)
	}
	for _, name := range []string{"k-multi", "k-none", "k-legacy", "k-shared"} {
		if got := keyTeam(name); got.Valid {
			t.Errorf("%s team = %v, want NULL", name, got)
		}
	}
}
