package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
)

// dsnForDatabase rewrites a DSN to point at another database on the same
// server (used for the maintenance connection and the disposable database).
func dsnForDatabase(dsn, database string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	parsed.Path = "/" + database
	return parsed.String()
}

// TestDomainUniquenessMigrationFailsClosedOnLegacyDuplicates builds a
// disposable database, migrates it to the pre-uniqueness schema, seeds legacy
// duplicate bindings (mixed casing, two owners), then applies the uniqueness
// migration and asserts every conflicting binding fails closed with its value
// preserved: no winner is chosen, the unrelated healthy binding stays enabled
// and the index rejects new conflicts.
func TestDomainUniquenessMigrationFailsClosedOnLegacyDuplicates(t *testing.T) {
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

	scratch := fmt.Sprintf("p6_migration_%d", time.Now().UnixNano())
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
	// The uniqueness migration is 00012; migrate to the newest pre-change
	// version first so the legacy rows can be seeded.
	if _, err := provider.UpTo(ctx, 11); err != nil {
		t.Fatalf("migrate to 00011: %v", err)
	}

	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UnixNano()
	// The schema is at 00011 here: the store's CreateUser (which also creates
	// the personal team of 00019) does not exist yet, so the two legacy users
	// are seeded directly.
	insertUser := func(email string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatalf("insert user %s: %v", email, err)
		}
		return id
	}
	userA := insertUser(fmt.Sprintf("p6-mig-a-%d@example.com", suffix))
	userB := insertUser(fmt.Sprintf("p6-mig-b-%d@example.com", suffix))
	var serverID pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("p6-mig-%d", suffix)).Scan(&serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	// Two legacy rows with the same normalized domain (different casing and
	// different owners) plus one healthy unrelated binding.
	insertApp := func(userID pgtype.UUID, name, domain string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO applications (user_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
			 VALUES ($1, $2, $3, 'https://github.com/acme/demo.git', 'main', 'dockerfile', $4, 80, 18080)`,
			userID, serverID, name, domain); err != nil {
			t.Fatalf("insert legacy app %s: %v", name, err)
		}
	}
	insertApp(userA, "legacy-a", "Legacy.Example.com")
	insertApp(userB, "legacy-b", "legacy.example.com")
	insertApp(userA, "healthy", "healthy.example.com")

	// Apply the uniqueness and history migrations, stopping at 00033: 00034
	// truncates the resource tables by design, which is outside this
	// test's scope.
	if _, err := provider.UpTo(ctx, 33); err != nil {
		t.Fatalf("migrate to 00033: %v", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT name, base_domain, base_domain_disabled FROM applications ORDER BY name`)
	if err != nil {
		t.Fatalf("read applications: %v", err)
	}
	defer rows.Close()
	type seededApp struct {
		domain   string
		disabled bool
	}
	apps := make(map[string]seededApp)
	for rows.Next() {
		var name, domain string
		var disabled bool
		if err := rows.Scan(&name, &domain, &disabled); err != nil {
			t.Fatalf("scan application: %v", err)
		}
		apps[name] = seededApp{domain: domain, disabled: disabled}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate applications: %v", err)
	}

	for _, name := range []string{"legacy-a", "legacy-b"} {
		app := apps[name]
		if !app.disabled {
			t.Fatalf("%s: disabled = false, want every legacy conflict to fail closed", name)
		}
		if app.domain != "legacy.example.com" {
			t.Fatalf("%s: domain = %q, want the preserved normalized value", name, app.domain)
		}
	}
	if healthy := apps["healthy"]; healthy.disabled || healthy.domain != "healthy.example.com" {
		t.Fatalf("healthy binding changed: %#v", healthy)
	}

	var indexExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'applications_server_domain_idx')`).Scan(&indexExists); err != nil {
		t.Fatalf("check index: %v", err)
	}
	if !indexExists {
		t.Fatal("applications_server_domain_idx was not created")
	}

	// A new enabled conflict against a live binding is rejected; the
	// conflicting legacy domain itself is free again because every old
	// binding holding it failed closed.
	var pgErr *pgconn.PgError
	_, err = pool.Exec(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
		 VALUES ($1, $1, $2, 'new-conflict', 'https://github.com/acme/demo.git', 'main', 'dockerfile', 'healthy.example.com', 80, 18081)`,
		userA, serverID)
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "applications_server_domain_idx" {
		t.Fatalf("new conflict err = %v, want a 23505 on applications_server_domain_idx", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
		 VALUES ($1, $1, $2, 'legacy-reclaim', 'https://github.com/acme/demo.git', 'main', 'dockerfile', 'legacy.example.com', 80, 18082)`,
		userA, serverID); err != nil {
		t.Fatalf("a disabled legacy domain must be free for a new active binding: %v", err)
	}
}
