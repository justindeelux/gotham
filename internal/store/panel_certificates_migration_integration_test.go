package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestPanelCertificatesMigrationPreservesExistingIntents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := testDSN()
	admin, err := sql.Open("pgx", dsnForDatabase(base, "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.PingContext(ctx); err != nil {
		if testDSNExplicit() {
			t.Fatal(err)
		}
		t.Skipf("Postgres unavailable: %v", err)
	}
	name := fmt.Sprintf("panel_cert_migration_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	dsn := dsnForDatabase(base, name)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	migrations, err := goose.NewProvider(goose.DialectPostgres, db, store.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.UpTo(ctx, 51); err != nil {
		t.Fatal(err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	st := store.New(pool)
	user, err := st.CreateUser(ctx, "panel-migration@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	team, environment, _ := seedEnvColumns(t, ctx, st, pool)
	var nodeID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name, ip, port, ssh_user) VALUES ('localhost', '127.0.0.1', 22, 'root') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{UserID: user.ID, TeamID: team, EnvironmentID: environment, ServerID: nodeID, Name: "existing-app", CloneUrl: "https://example.com/repo.git", Branch: "main", BuildPack: "dockerfile"})
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"app.example.com", "alias.example.com"} {
		if _, err := pool.Exec(ctx, `INSERT INTO domain_certificates (application_id,domain) VALUES ($1,$2)`, app.ID, domain); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	existing, err := st.ListDomainCertificatesByApplication(ctx, app.ID)
	if err != nil || len(existing) != 2 {
		t.Fatalf("existing certificates=%+v err=%v", existing, err)
	}
	for _, cert := range existing {
		if cert.OwnerKind == nil || *cert.OwnerKind != "application" || cert.ApplicationID != app.ID {
			t.Fatalf("changed existing intent: %+v", cert)
		}
	}
	panel, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{Domain: "panel.example.com", Enabled: true, Challenge: "http-01"})
	if err != nil {
		t.Fatal(err)
	}
	if panel.ApplicationID.Valid || panel.OwnerKind == nil || *panel.OwnerKind != "control_plane" {
		t.Fatalf("panel ownership=%+v", panel)
	}
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{Domain: "panel.example.com", Enabled: true, Challenge: "http-01"}); err == nil {
		t.Fatal("duplicate panel intent accepted")
	}
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{ApplicationID: app.ID, Domain: "app.example.com", Enabled: true, Challenge: "http-01"}); err == nil {
		t.Fatal("migration 46 application/domain uniqueness lost")
	}
	targets, err := st.ListCertificateStatusTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range targets {
		if target.CertificateID == panel.ID {
			found = true
			if target.ServerID != nodeID {
				t.Fatalf("panel status target=%+v", target)
			}
		}
	}
	if !found {
		t.Fatal("panel has no ACME status target")
	}
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{ApplicationID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Domain: "missing.example.com", Enabled: true, Challenge: "http-01"}); err == nil {
		t.Fatal("application foreign key lost")
	}
}
