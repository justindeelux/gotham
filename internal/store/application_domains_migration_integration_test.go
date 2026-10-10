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

// TestApplicationDomainsMigrationBackfillsAndConstrains builds a disposable
// database at the pre-JUS-89 schema, seeds legacy single-domain bindings
// (including a cross-node duplicate, which the old per-node index allowed),
// then applies the multi-domain migrations and asserts the backfill and the
// new invariants: every stored base_domain becomes the primary row, the
// oldest binding of a legacy conflict stays enabled while the newer one
// fails closed (disabled, value preserved), a domainless application gets no
// rows, and the three new indexes reject conflicting writes.
func TestApplicationDomainsMigrationBackfillsAndConstrains(t *testing.T) {
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

	scratch := fmt.Sprintf("p89_migration_%d", time.Now().UnixNano())
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
	if _, err := provider.UpTo(ctx, 44); err != nil {
		t.Fatalf("migrate to 00044: %v", err)
	}

	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano()
	insertUser := func(email string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", email, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO teams (id, name, is_personal) VALUES ($1, $2, true)`, id, email); err != nil {
			t.Fatalf("insert personal team %s: %v", email, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES ($1, $1, 'owner')`, id); err != nil {
			t.Fatalf("insert team member %s: %v", email, err)
		}
		return id
	}
	userA := insertUser(fmt.Sprintf("p89-mig-a-%d@example.com", suffix))
	userB := insertUser(fmt.Sprintf("p89-mig-b-%d@example.com", suffix))
	insertServer := func(name string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
			name).Scan(&id); err != nil {
			t.Fatalf("insert server %s: %v", name, err)
		}
		return id
	}
	serverA := insertServer(fmt.Sprintf("p89-mig-a-%d", suffix))
	serverB := insertServer(fmt.Sprintf("p89-mig-b-%d", suffix))
	var projectID string
	if err := pool.QueryRow(ctx, `INSERT INTO projects (team_id, name) VALUES ($1, $2) RETURNING id`,
		userA, fmt.Sprintf("p89-mig-%d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	var envID string
	if err := pool.QueryRow(ctx, `INSERT INTO environments (project_id, name) VALUES ($1, $2) RETURNING id`,
		projectID, fmt.Sprintf("p89-env-%d", suffix)).Scan(&envID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}

	insertApp := func(userID, serverID, name, domain, created string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, clone_url, branch, build_pack, base_domain, port, host_port, created_at)
			 VALUES ($1, $1, $2, $3, $4, 'https://github.com/acme/demo.git', 'main', 'dockerfile', $5, 80, 18080, $6)
			 RETURNING id`,
			userID, serverID, envID, name, domain, created).Scan(&id); err != nil {
			t.Fatalf("insert legacy app %s: %v", name, err)
		}
		return id
	}
	// The cross-node duplicate the old per-node index allowed: the oldest
	// binding must stay enabled, the newer one fails closed.
	appOld := insertApp(userA, serverA, "legacy-old", "shared.example.com", "2026-01-01T00:00:00Z")
	appNew := insertApp(userB, serverB, "legacy-new", "Shared.Example.com", "2026-02-01T00:00:00Z")
	appSolo := insertApp(userA, serverA, "healthy", "solo.example.com", "2026-01-15T00:00:00Z")
	appBare := insertApp(userA, serverA, "bare", "", "2026-01-20T00:00:00Z")

	if _, err := provider.UpTo(ctx, 46); err != nil {
		t.Fatalf("migrate to 00046: %v", err)
	}

	type domainRow struct {
		domain   string
		primary  bool
		disabled bool
	}
	rows, err := pool.Query(ctx,
		`SELECT application_id, domain, is_primary, disabled FROM application_domains`)
	if err != nil {
		t.Fatalf("read application_domains: %v", err)
	}
	defer rows.Close()
	byApp := make(map[string][]domainRow)
	for rows.Next() {
		var appID, domain string
		var primary, disabled bool
		if err := rows.Scan(&appID, &domain, &primary, &disabled); err != nil {
			t.Fatalf("scan domain row: %v", err)
		}
		byApp[appID] = append(byApp[appID], domainRow{domain, primary, disabled})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate domain rows: %v", err)
	}

	assertRows := func(appID string, want []domainRow) {
		t.Helper()
		got := byApp[appID]
		if len(got) != len(want) {
			t.Fatalf("app %s: domains = %#v, want %#v", appID, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("app %s: domains = %#v, want %#v", appID, got, want)
			}
		}
	}
	assertRows(appOld, []domainRow{{"shared.example.com", true, false}})
	assertRows(appNew, []domainRow{{"shared.example.com", true, true}})
	assertRows(appSolo, []domainRow{{"solo.example.com", true, false}})
	if len(byApp[appBare]) != 0 {
		t.Fatalf("domainless app has rows: %#v", byApp[appBare])
	}

	// The global index rejects a newly enabled conflicting binding.
	if _, err := pool.Exec(ctx,
		`INSERT INTO application_domains (application_id, domain, is_primary) VALUES ($1, 'SOLO.example.com', false)`,
		appNew); err == nil {
		t.Fatal("globally conflicting domain was accepted")
	}
	// The per-app index is case-insensitive.
	if _, err := pool.Exec(ctx,
		`INSERT INTO application_domains (application_id, domain) VALUES ($1, 'SOLO.EXAMPLE.COM')`,
		appSolo); err == nil {
		t.Fatal("duplicate domain inside one application was accepted")
	}
	// One primary per application.
	if _, err := pool.Exec(ctx,
		`INSERT INTO application_domains (application_id, domain, is_primary) VALUES ($1, 'second.example.com', true)`,
		appSolo); err == nil {
		t.Fatal("second primary domain was accepted")
	}
	// Certificates are unique per domain, not per application.
	if _, err := pool.Exec(ctx,
		`INSERT INTO domain_certificates (application_id, domain, challenge) VALUES ($1, 'solo.example.com', 'http-01')`,
		appSolo); err != nil {
		t.Fatalf("insert first certificate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO domain_certificates (application_id, domain, challenge) VALUES ($1, 'second.example.com', 'http-01')`,
		appSolo); err != nil {
		t.Fatalf("insert second-domain certificate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO domain_certificates (application_id, domain, challenge) VALUES ($1, 'solo.example.com', 'http-01')`,
		appSolo); err == nil {
		t.Fatal("duplicate per-domain certificate was accepted")
	}
}
