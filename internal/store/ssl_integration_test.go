package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestSSLCertificateStorage proves the BE-6.2 persistence contract against a
// real database: the sealed credential round-trips, the allowlist/zone/unique
// constraints hold, the one-certificate-per-application index holds, the
// provider/certificate reference guards reject dangling rows, and the routing
// query carries the certificate intent.
func TestSSLCertificateStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	explicit := testDSNExplicit()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres/migrations are unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("be-6.2-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p6-ssl-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", server.ID)
	})

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:     user.ID,
		ServerID:   server.ID,
		Name:       "ssl-app",
		CloneUrl:   "https://github.com/acme/demo.git",
		Branch:     "main",
		BuildPack:  "dockerfile",
		BaseDomain: "app.example.com",
		Port:       3000,
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}

	sealed, err := providers.SealSecret("integration-key", "cf-secret-token")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	provider, err := st.CreateDNSProvider(ctx, sqlc.CreateDNSProviderParams{
		Provider:   "cloudflare",
		Name:       "prod",
		Zones:      []string{"example.com"},
		Ciphertext: sealed,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if provider.Ciphertext == "cf-secret-token" {
		t.Fatal("credential stored in the clear")
	}
	opened, err := providers.OpenSecret("integration-key", provider.Ciphertext)
	if err != nil || opened != "cf-secret-token" {
		t.Fatalf("sealed credential round-trip = (%q, %v)", opened, err)
	}

	// At most one enabled provider per type.
	if _, err := st.CreateDNSProvider(ctx, sqlc.CreateDNSProviderParams{
		Provider:   "cloudflare",
		Zones:      []string{"other.com"},
		Ciphertext: sealed,
		Enabled:    true,
	}); err == nil {
		t.Fatal("second enabled provider of the same type was accepted")
	}
	// The allowlist is a schema constraint.
	if _, err := st.CreateDNSProvider(ctx, sqlc.CreateDNSProviderParams{
		Provider:   "route53",
		Zones:      []string{"example.net"},
		Ciphertext: sealed,
		Enabled:    false,
	}); err == nil {
		t.Fatal("provider outside the allowlist was accepted")
	}

	certificate, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: app.ID,
		Domain:        "app.example.com",
		Enabled:       true,
		Challenge:     "dns-01",
		DnsProviderID: provider.ID,
		Wildcard:      true,
	})
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	// One certificate config per application.
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: app.ID,
		Domain:        "app.example.com",
		Enabled:       false,
		Challenge:     "http-01",
	}); err == nil {
		t.Fatal("second certificate config for the same application was accepted")
	}
	// Wildcard is dns-01 only and dns-01 needs a provider.
	app2, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:     user.ID,
		ServerID:   server.ID,
		Name:       "ssl-app-wild",
		CloneUrl:   "https://github.com/acme/demo.git",
		Branch:     "main",
		BuildPack:  "dockerfile",
		BaseDomain: "wild.example.com",
		Port:       3000,
	})
	if err != nil {
		t.Fatalf("create second application: %v", err)
	}
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: app2.ID,
		Domain:        "wild.example.com",
		Enabled:       true,
		Challenge:     "http-01",
		Wildcard:      true,
	}); err == nil {
		t.Fatal("wildcard with http-01 was accepted")
	}

	// A referenced provider cannot be deleted (foreign key).
	if err := st.DeleteDNSProvider(ctx, provider.ID); err == nil {
		t.Fatal("deleting a referenced provider was accepted")
	}
	if _, err := st.CountDomainCertificatesByProvider(ctx, provider.ID); err != nil {
		t.Fatalf("count certificates: %v", err)
	}
	count, err := st.CountEnabledDomainCertificatesByProvider(ctx, provider.ID)
	if err != nil || count != 1 {
		t.Fatalf("enabled count = %d (%v), want 1", count, err)
	}

	// The routing query carries the certificate intent for the application.
	rows, err := st.ListProxiedApplications(ctx)
	if err != nil {
		t.Fatalf("list proxied applications: %v", err)
	}
	var found bool
	for _, row := range rows {
		if uuidFromPGType(row.ID) != uuidFromPGType(app.ID) {
			continue
		}
		found = true
		if !row.CertificateConfigured || row.CertificateDomain != "app.example.com" ||
			row.CertificateChallenge != "dns-01" || !row.CertificateWildcard ||
			uuidFromPGType(row.CertificateDnsProviderID) != uuidFromPGType(provider.ID) {
			t.Fatalf("certificate columns = %#v", row)
		}
	}
	if !found {
		t.Fatal("proxied application row missing")
	}

	// Deleting the application cascades the certificate config, which then
	// releases the provider.
	if err := st.DeleteApplication(ctx, app.ID); err != nil {
		t.Fatalf("delete application: %v", err)
	}
	if _, err := st.GetDomainCertificate(ctx, certificate.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("certificate survived the application cascade: %v", err)
	}
	if err := st.DeleteDNSProvider(ctx, provider.ID); err != nil {
		t.Fatalf("delete unreferenced provider: %v", err)
	}
}

// uuidFromPGType renders a pgtype.UUID for comparison without importing the
// proxy package.
func uuidFromPGType(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
