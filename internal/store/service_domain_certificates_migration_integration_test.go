package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestServiceDomainCertificateMigration(t *testing.T) {
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
	name := fmt.Sprintf("svccert%d%d", time.Now().UnixNano(), os.Getpid())
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
	if _, err := provider.UpTo(ctx, 92); err != nil {
		t.Fatalf("migrate to 92: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect scratch database: %v", err)
	}
	var serviceOne, serviceTwo, applicationID string
	err = conn.QueryRow(ctx, `
		WITH u AS (INSERT INTO users (email) VALUES ('svc-cert@example.com') RETURNING id),
		     p AS (INSERT INTO projects (name) VALUES ('svc-cert') RETURNING id),
		     e AS (INSERT INTO environments (project_id, name) SELECT id, 'production' FROM p RETURNING id),
		     s AS (INSERT INTO servers (name, ip, port, ssh_user) VALUES ('svc-cert-node', '127.0.0.1', 22, 'root') RETURNING id),
		     sv1 AS (
		         INSERT INTO services (user_id, server_id, environment_id, name, compose_yaml)
		         SELECT u.id, s.id, e.id, 'svc-one', 'services: {}' FROM u, s, e
		         RETURNING id
		     ),
		     sv2 AS (
		         INSERT INTO services (user_id, server_id, environment_id, name, compose_yaml)
		         SELECT u.id, s.id, e.id, 'svc-two', 'services: {}' FROM u, s, e
		         RETURNING id
		     ),
		     app AS (
		         INSERT INTO applications (user_id, server_id, environment_id, name)
		         SELECT u.id, s.id, e.id, 'svc-cert-app' FROM u, s, e
		         RETURNING id
		     )
		SELECT (SELECT id::text FROM sv1), (SELECT id::text FROM sv2), (SELECT id::text FROM app)`).
		Scan(&serviceOne, &serviceTwo, &applicationID)
	if err != nil {
		t.Fatalf("seed scratch rows: %v", err)
	}
	_ = conn.Close(ctx)

	if _, err := provider.UpTo(ctx, 93); err != nil {
		t.Fatalf("migrate to 93: %v", err)
	}

	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	serviceOneID := pgUUID(uuid.MustParse(serviceOne))
	serviceTwoID := pgUUID(uuid.MustParse(serviceTwo))
	applicationUUID := pgUUID(uuid.MustParse(applicationID))

	created, err := st.CreateServiceDomainCertificate(ctx, sqlc.CreateServiceDomainCertificateParams{
		ServiceID: serviceOneID,
		Domain:    "svc.example.com",
		Enabled:   true,
		Challenge: "http-01",
	})
	if err != nil {
		t.Fatalf("CreateServiceDomainCertificate: %v", err)
	}
	if created.ServiceID != serviceOneID || created.ApplicationID.Valid {
		t.Fatalf("created = %#v, want a service-owned row", created)
	}
	if created.OwnerKind == nil || *created.OwnerKind != "service" {
		t.Fatalf("owner_kind = %v, want service", created.OwnerKind)
	}

	inserted, err := st.CreateServiceDomainCertificateIfAbsent(ctx, sqlc.CreateServiceDomainCertificateIfAbsentParams{
		ServiceID: serviceOneID,
		Domain:    "svc.example.com",
		Enabled:   true,
		Challenge: "http-01",
	})
	if err != nil || inserted != 0 {
		t.Fatalf("CreateServiceDomainCertificateIfAbsent = %d / %v, want a no-op", inserted, err)
	}

	second, err := st.CreateServiceDomainCertificate(ctx, sqlc.CreateServiceDomainCertificateParams{
		ServiceID: serviceTwoID,
		Domain:    "svc.example.com",
		Enabled:   true,
		Challenge: "http-01",
	})
	if err != nil {
		t.Fatalf("the same host on another service: %v", err)
	}
	if second.ServiceID != serviceTwoID {
		t.Fatalf("second owner = %s", second.ServiceID)
	}

	rows, err := st.ListDomainCertificatesByService(ctx, serviceOneID)
	if err != nil {
		t.Fatalf("ListDomainCertificatesByService: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != created.ID {
		t.Fatalf("service rows = %#v, want only the first service's intent", rows)
	}

	proxied, err := st.ListProxiedServiceCertificates(ctx)
	if err != nil {
		t.Fatalf("ListProxiedServiceCertificates: %v", err)
	}
	if len(proxied) != 2 {
		t.Fatalf("proxied service intents = %#v, want two", proxied)
	}

	if _, err := st.CreateServiceDomainCertificate(ctx, sqlc.CreateServiceDomainCertificateParams{
		ServiceID: serviceOneID,
		Domain:    "svc.example.com",
		Enabled:   true,
		Challenge: "http-01",
	}); err == nil {
		t.Fatal("duplicate service domain was accepted")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("duplicate err = %v, want a unique violation", err)
		}
	}

	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: applicationUUID,
		Domain:        "app.example.com",
		Enabled:       true,
		Challenge:     "http-01",
	}); err != nil {
		t.Fatalf("application certificate: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO domain_certificates (application_id, service_id, domain, enabled, challenge)
		VALUES ($1, $2, 'both.example.com', true, 'http-01')`,
		applicationUUID, serviceOneID); err == nil {
		t.Fatal("a row with both owners was accepted")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("both-owner err = %v, want a check violation", err)
		}
	}

	if deleted, err := st.DeleteAutoServiceDomainCertificate(ctx, sqlc.DeleteAutoServiceDomainCertificateParams{
		ServiceID: serviceOneID,
		Domain:    "svc.example.com",
	}); err != nil || deleted != 1 {
		t.Fatalf("DeleteAutoServiceDomainCertificate = %d / %v, want one row", deleted, err)
	}
	if err := st.DeleteServiceDomainCertificates(ctx, serviceTwoID); err != nil {
		t.Fatalf("DeleteServiceDomainCertificates: %v", err)
	}
	remaining, err := st.ListDomainCertificates(ctx)
	if err != nil {
		t.Fatalf("ListDomainCertificates: %v", err)
	}
	for _, row := range remaining {
		if row.ServiceID.Valid {
			t.Fatalf("service intent survived delete: %#v", row)
		}
	}
}
