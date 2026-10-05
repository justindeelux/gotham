package deploy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestCreatePreviewApplicationClonesCertificateIntent proves the HTTPS intent
// clone against a real database: the repository adapter reads the base's
// wildcard intent, the sibling gets its own row recorded for the preview host,
// and the system teardown removes it through the application cascade. It skips
// when no database is reachable.
func TestCreatePreviewApplicationClonesCertificateIntent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	explicit := integrationDSNExplicit()
	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
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
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	const secret = "integration-secret"
	st := store.New(pool)
	repo := newStoreRepository(st, secret)

	suffix := time.Now().UnixNano()
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p8-preview-cert-%d", suffix),
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
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	// The provider cleanup runs after the user cleanup (t.Cleanup is LIFO):
	// deleting the user cascades the certificate rows, which releases the
	// provider's RESTRICT reference.
	sealed, err := providers.SealSecret(secret, "cf-token")
	if err != nil {
		t.Fatalf("seal provider credential: %v", err)
	}
	providerRow, err := st.CreateDNSProvider(ctx, sqlc.CreateDNSProviderParams{
		// digitalocean: the store integration tests hold an enabled cloudflare
		// provider while they run in a parallel package process.
		Provider:   "digitalocean",
		Name:       fmt.Sprintf("p8-preview-%d", suffix),
		Zones:      []string{"example.com"},
		Ciphertext: sealed,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create DNS provider: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM dns_providers WHERE id = $1", providerRow.ID); err != nil {
			t.Logf("cleanup provider: %v", err)
		}
	})

	user, err := st.CreateUser(ctx, fmt.Sprintf("p8-preview-cert-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	teamID, envID, _ := seedProjectEnvironment(t, ctx, st)
	baseDomain := fmt.Sprintf("p8-%d.example.com", suffix)
	base, err := repo.CreateApplication(ctx, Application{
		UserID:        userID,
		TeamID:        teamID,
		ServerID:      uuid.UUID(serverRow.ID.Bytes),
		EnvironmentID: envID,
		Name:          fmt.Sprintf("p8-preview-%d", suffix),
		Provider:      "github",
		Repo:          "octo/gotham",
		CloneURL:      "https://github.com/octo/gotham.git",
		Branch:        "main",
		BuildPack:     "auto",
		BaseDomain:    baseDomain,
		Port:          3000,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create base application: %v", err)
	}
	if _, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: pgUUID(base.ID),
		Domain:        baseDomain,
		Enabled:       true,
		Challenge:     "dns-01",
		DnsProviderID: providerRow.ID,
		Wildcard:      true,
	}); err != nil {
		t.Fatalf("create base certificate intent: %v", err)
	}

	svc := NewService(Config{Repository: repo, Secret: secret, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })

	host := fmt.Sprintf("pr-7-preview.p8-%d.example.com", suffix)
	created, err := svc.CreatePreviewApplication(ctx, base.ID, PreviewApplicationInput{
		Name:       fmt.Sprintf("p8-preview-%d-pr-7", suffix),
		Branch:     "feat/x",
		BaseDomain: host,
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	intent, err := repo.GetCertificateIntent(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCertificateIntent(sibling): %v", err)
	}
	if intent.Domain != host || !intent.Enabled || !intent.Wildcard ||
		intent.Challenge != "dns-01" || intent.DNSProviderID != uuid.UUID(providerRow.ID.Bytes) {
		t.Fatalf("cloned intent = %+v, want the base's wildcard configuration recorded for %s", intent, host)
	}

	// The teardown removes the sibling and its intent (the schema cascade).
	if err := svc.DeleteSystemApplication(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSystemApplication: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM domain_certificates WHERE application_id = $1",
		pgUUID(created.ID),
	).Scan(&remaining); err != nil {
		t.Fatalf("count sibling intents: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("sibling intents after the teardown = %d, want none", remaining)
	}
	if _, err := repo.GetCertificateIntent(ctx, base.ID); err != nil {
		t.Fatalf("base intent after the teardown: %v", err)
	}
}
