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

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestDomainRedirectStorage proves the BE-6.3 persistence contract against a
// real database: the redirect rule round-trips, the source uniqueness and the
// code/self-redirect constraints hold, the application join carries the node
// state for generation, and deleting the application cascades its rules away.
func TestDomainRedirectStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	explicit := testDSNExplicit()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("be-6.3-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p6-redirects-%d", suffix),
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
		Name:       "redirect-app",
		CloneUrl:   "https://github.com/acme/demo.git",
		Branch:     "main",
		BuildPack:  "dockerfile",
		BaseDomain: "app.example.com",
		Port:       3000,
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}

	rule, err := st.CreateDomainRedirect(ctx, sqlc.CreateDomainRedirectParams{
		ApplicationID: app.ID,
		SourceDomain:  "old.example.com",
		TargetDomain:  "app.example.com",
		Code:          301,
		PreservePath:  true,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create redirect: %v", err)
	}
	if rule.Code != 301 || !rule.PreservePath || !rule.Enabled {
		t.Fatalf("stored redirect = %#v", rule)
	}

	// The source host is claimed exactly once.
	if _, err := st.CreateDomainRedirect(ctx, sqlc.CreateDomainRedirectParams{
		ApplicationID: app.ID,
		SourceDomain:  "old.example.com",
		TargetDomain:  "elsewhere.example.com",
		Code:          302,
		PreservePath:  false,
		Enabled:       true,
	}); err == nil {
		t.Fatal("second rule with the same source was accepted")
	}
	// Only 301/302 are expressible.
	if _, err := st.CreateDomainRedirect(ctx, sqlc.CreateDomainRedirectParams{
		ApplicationID: app.ID,
		SourceDomain:  "temp.example.com",
		TargetDomain:  "app.example.com",
		Code:          307,
		Enabled:       true,
	}); err == nil {
		t.Fatal("redirect code outside 301/302 was accepted")
	}
	// A self redirect cannot be stored.
	if _, err := st.CreateDomainRedirect(ctx, sqlc.CreateDomainRedirectParams{
		ApplicationID: app.ID,
		SourceDomain:  "loop.example.com",
		TargetDomain:  "loop.example.com",
		Code:          301,
		Enabled:       true,
	}); err == nil {
		t.Fatal("self redirect was accepted")
	}
	// An unknown application cannot own a rule.
	if _, err := st.CreateDomainRedirect(ctx, sqlc.CreateDomainRedirectParams{
		ApplicationID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
		SourceDomain:  "orphan.example.com",
		TargetDomain:  "app.example.com",
		Code:          301,
		Enabled:       true,
	}); err == nil {
		t.Fatal("rule for a missing application was accepted")
	}

	// Update is a partial write; the source claim moves with it.
	updated, err := st.UpdateDomainRedirect(ctx, sqlc.UpdateDomainRedirectParams{
		ID:           rule.ID,
		SourceDomain: "legacy.example.com",
		TargetDomain: "app.example.com",
		Code:         302,
		PreservePath: false,
		Enabled:      false,
	})
	if err != nil {
		t.Fatalf("update redirect: %v", err)
	}
	if updated.SourceDomain != "legacy.example.com" || updated.Code != 302 || updated.PreservePath || updated.Enabled {
		t.Fatalf("updated redirect = %#v", updated)
	}
	if !updated.UpdatedAt.Valid || !updated.UpdatedAt.Time.After(updated.CreatedAt.Time) {
		t.Fatalf("updated_at not advanced: %#v", updated)
	}

	bySource, err := st.GetDomainRedirectBySource(ctx, "legacy.example.com")
	if err != nil || uuidFromPGType(bySource.ID) != uuidFromPGType(rule.ID) {
		t.Fatalf("by-source lookup = (%v, %v)", bySource.ID, err)
	}
	byApplication, err := st.ListDomainRedirectsByApplication(ctx, app.ID)
	if err != nil || len(byApplication) != 1 {
		t.Fatalf("by-application list = %d (%v), want 1", len(byApplication), err)
	}
	if _, err := st.GetDomainRedirect(ctx, rule.ID); err != nil {
		t.Fatalf("get redirect: %v", err)
	}

	// The generation join carries the owning node and the disabled flag.
	generationRules, err := st.ListRedirectRules(ctx)
	if err != nil {
		t.Fatalf("list redirect rules: %v", err)
	}
	var found bool
	for _, row := range generationRules {
		if uuidFromPGType(row.ID) != uuidFromPGType(rule.ID) {
			continue
		}
		found = true
		if uuidFromPGType(row.ServerID) != uuidFromPGType(server.ID) || row.BaseDomainDisabled {
			t.Fatalf("generation row = %#v", row)
		}
	}
	if !found {
		t.Fatal("redirect rule missing from the generation join")
	}

	// Ownership guards read the application domains and enabled sources.
	domains, err := st.ListApplicationBaseDomains(ctx)
	if err != nil {
		t.Fatalf("list application base domains: %v", err)
	}
	var sawApp bool
	for _, domain := range domains {
		if uuidFromPGType(domain.ID) == uuidFromPGType(app.ID) && domain.BaseDomain == "app.example.com" {
			sawApp = true
		}
	}
	if !sawApp {
		t.Fatal("application base domain missing from the ownership guard query")
	}
	sources, err := st.ListEnabledRedirects(ctx)
	if err != nil {
		t.Fatalf("list enabled redirects: %v", err)
	}
	enabledCount := 0
	for _, source := range sources {
		if uuidFromPGType(source.ID) == uuidFromPGType(rule.ID) {
			enabledCount++
		}
	}
	if enabledCount != 0 {
		t.Fatal("a disabled rule appeared as an enabled source")
	}

	// The status target join pairs certificate intents with their node.
	certificate, err := st.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: app.ID,
		Domain:        "app.example.com",
		Enabled:       true,
		Challenge:     "http-01",
	})
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	targets, err := st.ListCertificateStatusTargets(ctx)
	if err != nil {
		t.Fatalf("list certificate status targets: %v", err)
	}
	var sawTarget bool
	for _, target := range targets {
		if uuidFromPGType(target.CertificateID) == uuidFromPGType(certificate.ID) {
			sawTarget = true
			if target.Domain != "app.example.com" || uuidFromPGType(target.ServerID) != uuidFromPGType(server.ID) {
				t.Fatalf("status target = %#v", target)
			}
		}
	}
	if !sawTarget {
		t.Fatal("certificate status target missing")
	}

	// Deleting the application cascades the redirect away.
	if err := st.DeleteApplication(ctx, app.ID); err != nil {
		t.Fatalf("delete application: %v", err)
	}
	if _, err := st.GetDomainRedirect(ctx, rule.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("redirect survived the application cascade: %v", err)
	}
	if _, err := st.GetDomainCertificate(ctx, certificate.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("certificate survived the application cascade: %v", err)
	}
}
