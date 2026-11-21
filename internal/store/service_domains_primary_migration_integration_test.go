package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
)

func TestServiceDomainsPrimaryMigrationBackfill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	base := testDSN()
	if err := store.ProbeOnce(ctx, base); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	name := fmt.Sprintf("sdprimary%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect for scratch database: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		admin, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})
	parsed.Path = "/" + name
	dsn := parsed.String()

	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = sqldb.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, store.Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if _, err := provider.UpTo(ctx, 91); err != nil {
		t.Fatalf("migrate to 91: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	var serviceID string
	err = conn.QueryRow(ctx, `
		WITH u AS (INSERT INTO users (email) VALUES ('sd-primary@example.com') RETURNING id),
		     p AS (INSERT INTO projects (name) VALUES ('sd-primary') RETURNING id),
		     e AS (INSERT INTO environments (project_id, name) SELECT id, 'production' FROM p RETURNING id),
		     s AS (INSERT INTO servers (name, ip, port, ssh_user) VALUES ('sd-primary-node', '127.0.0.1', 22, 'root') RETURNING id),
		     sv AS (
		         INSERT INTO services (user_id, server_id, environment_id, name, compose_yaml)
		         SELECT u.id, s.id, e.id, 'sd-primary', 'services: {}' FROM u, s, e
		         RETURNING id
		     )
		SELECT id::text FROM sv`).Scan(&serviceID)
	if err != nil {
		t.Fatalf("seed service: %v", err)
	}
	for _, row := range []struct {
		host      string
		createdAt string
		disabled  bool
	}{
		{"oldest.example.com", "2024-01-01T00:00:00Z", false},
		{"newer.example.com", "2024-02-01T00:00:00Z", false},
		{"disabled-oldest.example.com", "2023-01-01T00:00:00Z", true},
	} {
		if _, err := conn.Exec(ctx, `
			INSERT INTO service_domains (service_id, compose_service, port, domain, protocol, disabled, created_at)
			VALUES ($1::uuid, 'web', 80, $2, 'http', $3, $4)`,
			serviceID, row.host, row.disabled, row.createdAt); err != nil {
			t.Fatalf("seed domain %s: %v", row.host, err)
		}
	}
	_ = conn.Close(ctx)

	if _, err := provider.UpTo(ctx, 92); err != nil {
		t.Fatalf("migrate to 92: %v", err)
	}
	rows, err := sqldb.QueryContext(ctx, `
		SELECT domain, is_primary FROM service_domains WHERE service_id = $1::uuid ORDER BY created_at`, serviceID)
	if err != nil {
		t.Fatalf("read backfill: %v", err)
	}
	defer func() { _ = rows.Close() }()
	primaries := []string{}
	for rows.Next() {
		var host string
		var primary bool
		if err := rows.Scan(&host, &primary); err != nil {
			t.Fatalf("scan backfill: %v", err)
		}
		if primary {
			primaries = append(primaries, host)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read backfill: %v", err)
	}
	if len(primaries) != 1 || primaries[0] != "oldest.example.com" {
		t.Fatalf("primaries = %v, want [oldest.example.com]", primaries)
	}
}
