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
	"github.com/justindeelux/gotham/internal/store/sqlc"
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
		t.Skipf("Postgres not available: %v", err)
	}

	scratch := fmt.Sprintf("p6_migration_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+scratch); err != nil {
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
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	userA, err := st.CreateUser(ctx, fmt.Sprintf("p6-mig-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	userB, err := st.CreateUser(ctx, fmt.Sprintf("p6-mig-b-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p6-mig-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	// Two legacy rows with the same normalized domain (different casing and
	// different owners) plus one healthy unrelated binding.
	insertApp := func(userID pgtype.UUID, name, domain string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO applications (user_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
			 VALUES ($1, $2, $3, 'https://github.com/acme/demo.git', 'main', 'dockerfile', $4, 80, 18080)`,
			userID, server.ID, name, domain); err != nil {
			t.Fatalf("insert legacy app %s: %v", name, err)
		}
	}
	insertApp(userA.ID, "legacy-a", "Legacy.Example.com")
	insertApp(userB.ID, "legacy-b", "legacy.example.com")
	insertApp(userA.ID, "healthy", "healthy.example.com")

	// Apply the uniqueness and history migrations.
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to latest: %v", err)
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
		`INSERT INTO applications (user_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
		 VALUES ($1, $2, 'new-conflict', 'https://github.com/acme/demo.git', 'main', 'dockerfile', 'healthy.example.com', 80, 18081)`,
		userA.ID, server.ID)
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "applications_server_domain_idx" {
		t.Fatalf("new conflict err = %v, want a 23505 on applications_server_domain_idx", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO applications (user_id, server_id, name, clone_url, branch, build_pack, base_domain, port, host_port)
		 VALUES ($1, $2, 'legacy-reclaim', 'https://github.com/acme/demo.git', 'main', 'dockerfile', 'legacy.example.com', 80, 18082)`,
		userA.ID, server.ID); err != nil {
		t.Fatalf("a disabled legacy domain must be free for a new active binding: %v", err)
	}
}
