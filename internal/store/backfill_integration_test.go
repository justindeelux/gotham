package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestBackfillApplicationCertificates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	explicit := testDSNExplicit()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("jus-201-backfill-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	teamID, envID, serverID := seedEnvColumns(t, ctx, st, pool)

	newApp := func(name string) sqlc.Application {
		t.Helper()
		app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
			UserID: user.ID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
			Name:      name,
			CloneUrl:  "https://github.com/acme/demo.git",
			Branch:    "main",
			BuildPack: "dockerfile",
		})
		if err != nil {
			t.Fatalf("create application %s: %v", name, err)
		}
		return app
	}
	addDomain := func(appID pgtype.UUID, domain string, primary, disabled bool, protocol string) {
		t.Helper()
		if _, err := st.CreateApplicationDomain(ctx, sqlc.CreateApplicationDomainParams{
			ApplicationID: appID, Domain: domain, IsPrimary: primary, Disabled: disabled,
			Port: 3000, Protocol: protocol,
		}); err != nil {
			t.Fatalf("create domain %s: %v", domain, err)
		}
	}

	plain := newApp("bf-plain")
	addDomain(plain.ID, fmt.Sprintf("plain-%d.example.com", suffix), true, false, "https")

	disabled := newApp("bf-disabled")
	addDomain(disabled.ID, fmt.Sprintf("disabled-%d.example.com", suffix), true, true, "https")

	httpOnly := newApp("bf-http")
	addDomain(httpOnly.ID, fmt.Sprintf("http-%d.example.com", suffix), true, false, "http")

	wild := newApp("bf-wild")
	addDomain(wild.ID, fmt.Sprintf("wild-%d.example.com", suffix), true, false, "https")
	addDomain(wild.ID, fmt.Sprintf("sibling-%d.example.com", suffix), false, false, "https")
	sealed, err := providers.SealSecret("integration-key", "do-token")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	provider, err := st.CreateDNSProvider(ctx, sqlc.CreateDNSProviderParams{
		Provider: "digitalocean", Zones: []string{"example.com"}, Ciphertext: sealed, Enabled: false,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM dns_providers WHERE id = $1", provider.ID)
	})
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: wild.ID, Domain: fmt.Sprintf("wild-%d.example.com", suffix),
		Enabled: true, Challenge: "dns-01", DnsProviderID: provider.ID, Wildcard: true,
	}); err != nil {
		t.Fatalf("create wildcard intent: %v", err)
	}

	if _, err := st.BackfillApplicationCertificates(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	certs, err := st.ListDomainCertificatesByApplication(ctx, plain.ID)
	if err != nil {
		t.Fatalf("list plain intents: %v", err)
	}
	if len(certs) != 1 || !certs[0].Enabled || certs[0].Challenge != "http-01" || certs[0].Wildcard {
		t.Fatalf("plain intents = %#v, want one enabled http-01 default", certs)
	}

	certs, err = st.ListDomainCertificatesByApplication(ctx, httpOnly.ID)
	if err != nil {
		t.Fatalf("list http intents: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("http domain intents = %#v, want none", certs)
	}

	certs, err = st.ListDomainCertificatesByApplication(ctx, disabled.ID)
	if err != nil {
		t.Fatalf("list disabled intents: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("disabled domain intents = %#v, want none", certs)
	}

	certs, err = st.ListDomainCertificatesByApplication(ctx, wild.ID)
	if err != nil {
		t.Fatalf("list wildcard app intents: %v", err)
	}
	if len(certs) != 1 || !certs[0].Wildcard {
		t.Fatalf("wildcard app intents = %#v, want only the operator wildcard", certs)
	}

	if _, err := st.BackfillApplicationCertificates(ctx); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	certs, err = st.ListDomainCertificatesByApplication(ctx, plain.ID)
	if err != nil {
		t.Fatalf("list plain intents after rerun: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("rerun intents = %#v, want the first run's row only", certs)
	}
}
