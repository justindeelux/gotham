package deploy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

// openDomainIntegrationStore opens the integration database with migrations
// applied, or skips when Postgres is unreachable (fails when explicitly
// configured but down).
func openDomainIntegrationStore(t *testing.T, ctx context.Context) *store.Store {
	t.Helper()
	dsn := integrationDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if integrationDSNExplicit() {
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
	return store.New(pool)
}

// domainIntegrationService builds a Service over the real repository with a
// team-owner scope for a freshly created user.
func domainIntegrationService(t *testing.T, ctx context.Context, st *store.Store, teamID uuid.UUID) (*Service, context.Context, uuid.UUID) {
	t.Helper()
	userRow, err := st.CreateUser(ctx, fmt.Sprintf("jus233-domains-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uuid.UUID(userRow.ID.Bytes)
	repo := newStoreRepository(st, "integration-secret")
	svc := NewService(Config{Repository: repo, Secret: "integration-secret", Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })
	svcCtx := teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: teams.RoleOwner})
	return svc, svcCtx, userID
}

func createDomainIntegrationApp(t *testing.T, ctx context.Context, svc *Service, userID, envID, serverID uuid.UUID, name, domain string) Application {
	t.Helper()
	created, err := svc.CreateApplication(ctx, userID, CreateApplicationInput{
		Name:          name,
		EnvironmentID: envID,
		Provider:      "github",
		Repo:          "acme/demo",
		CloneURL:      "https://github.com/acme/demo.git",
		Branch:        "main",
		BuildPack:     "dockerfile",
		ServerID:      serverID,
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	if domain != "" {
		if _, err := svc.AddDomain(ctx, userID, created.ID, DomainInput{Domain: domain, Port: 3000, Protocol: ProtocolHTTPS}); err != nil {
			t.Fatalf("seed primary domain: %v", err)
		}
	}
	return created
}

func primaryOf(t *testing.T, domains []ApplicationDomain) ApplicationDomain {
	t.Helper()
	var found *ApplicationDomain
	for i := range domains {
		if domains[i].IsPrimary {
			if found != nil {
				t.Fatalf("two primaries in %#v", domains)
			}
			found = &domains[i]
		}
	}
	if found == nil {
		t.Fatalf("no primary in %#v", domains)
	}
	return *found
}

// TestDomainPromotionFlipFlopRealDB promotes, flips back and promotes again
// on a real database: the demote-then-promote transaction must keep exactly
// one primary on every flip.
func TestDomainPromotionFlipFlopRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)

	app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "flip", "a.example.com")
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, DomainInput{Domain: "b.example.com", Port: 3000, Protocol: ProtocolHTTPS}); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	domains, err := svc.ListDomains(svcCtx, userID, app.ID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	var aliasID uuid.UUID
	for _, domain := range domains {
		if !domain.IsPrimary {
			aliasID = domain.ID
		}
	}
	flip := func(target uuid.UUID, want string) {
		t.Helper()
		if err := svc.SetPrimaryDomain(svcCtx, userID, app.ID, target); err != nil {
			t.Fatalf("set primary %s: %v", want, err)
		}
		rows, err := svc.ListDomains(svcCtx, userID, app.ID)
		if err != nil {
			t.Fatalf("list domains: %v", err)
		}
		if primaryOf(t, rows).Domain != want {
			t.Fatalf("primary = %#v, want %q", rows, want)
		}
	}
	flip(aliasID, "b.example.com")
	flip(domains[0].ID, "a.example.com")
	flip(aliasID, "b.example.com")
	flip(domains[0].ID, "a.example.com")
}

// TestRemoveDomainPromotesEnabledRealDB proves removal promotes the oldest
// ENABLED row, and only falls back to a disabled row when none remain.
func TestRemoveDomainPromotesEnabledRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)

	app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "promote", "old.example.com")
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, DomainInput{Domain: "disabled.example.com", Port: 3000, Protocol: ProtocolHTTP}); err != nil {
		t.Fatalf("add loser: %v", err)
	}
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, DomainInput{Domain: "new.example.com", Port: 3000, Protocol: ProtocolHTTP}); err != nil {
		t.Fatalf("add winner: %v", err)
	}
	rows, err := svc.ListDomains(svcCtx, userID, app.ID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	byName := make(map[string]ApplicationDomain)
	for _, row := range rows {
		byName[row.Domain] = row
	}
	// Simulate a migration loser: disable the middle row directly.
	loser := byName["disabled.example.com"]
	if _, err := st.UpdateApplicationDomain(ctx, sqlc.UpdateApplicationDomainParams{
		ID:        pgUUID(loser.ID),
		Domain:    loser.Domain,
		IsPrimary: loser.IsPrimary,
		Disabled:  true,
		Port:      loser.Port,
		Protocol:  loser.Protocol,
	}); err != nil {
		t.Fatalf("disable row: %v", err)
	}

	if err := svc.RemoveDomain(svcCtx, userID, app.ID, byName["old.example.com"].ID); err != nil {
		t.Fatalf("remove primary: %v", err)
	}
	rows, err = svc.ListDomains(svcCtx, userID, app.ID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	if primaryOf(t, rows).Domain != "new.example.com" {
		t.Fatalf("primary = %#v, want the enabled row promoted", rows)
	}

	if err := svc.RemoveDomain(svcCtx, userID, app.ID, byName["new.example.com"].ID); err != nil {
		t.Fatalf("remove promoted: %v", err)
	}
	rows, err = svc.ListDomains(svcCtx, userID, app.ID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	if promoted := primaryOf(t, rows); promoted.Domain != "disabled.example.com" || !promoted.Disabled {
		t.Fatalf("primary = %#v, want the disabled fallback carried visibly", promoted)
	}
}

// TestAddDomainReattachRealDB is the JUS-201 regression: attach, remove and
// re-attach the same host against real Postgres. The earlier code left the
// auto-created intent behind, so the re-attach hit the (application_id,
// domain) unique index inside the attach transaction and the whole domain row
// rolled back.
func TestAddDomainReattachRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)
	repo := newStoreRepository(st, "integration-secret")

	app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "reattach", "base.example.com")
	attached, err := svc.AddDomain(svcCtx, userID, app.ID, DomainInput{Domain: "reattach.example.com", Port: 3000, Protocol: ProtocolHTTPS})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	intents, err := repo.ListCertificateIntents(ctx, app.ID)
	if err != nil {
		t.Fatalf("list intents: %v", err)
	}
	if len(intents) != 2 {
		t.Fatalf("attach intents = %#v, want one per https domain", intents)
	}

	if err := svc.RemoveDomain(svcCtx, userID, app.ID, attached.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	intents, err = repo.ListCertificateIntents(ctx, app.ID)
	if err != nil {
		t.Fatalf("list intents after remove: %v", err)
	}
	if len(intents) != 1 || intents[0].Domain != "base.example.com" {
		t.Fatalf("auto intent survived removal: %#v", intents)
	}

	if _, err := svc.AddDomain(svcCtx, userID, app.ID, DomainInput{Domain: "reattach.example.com", Port: 3000, Protocol: ProtocolHTTPS}); err != nil {
		t.Fatalf("re-attach: %v", err)
	}
	intents, err = repo.ListCertificateIntents(ctx, app.ID)
	if err != nil {
		t.Fatalf("list intents after re-attach: %v", err)
	}
	if len(intents) != 2 {
		t.Fatalf("re-attach intents = %#v", intents)
	}
}

// TestAddDomainPreExistingIntentRealDB proves the attach transaction survives a
// host that already has an operator intent.
func TestAddDomainPreExistingIntentRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)
	repo := newStoreRepository(st, "integration-secret")

	for _, tc := range []struct {
		name   string
		base   string
		domain string
	}{
		{"alias", "base.example.com", "kept-alias.example.com"},
		{"primary", "", "kept-primary.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "keep-"+tc.name, tc.base)
			if err := repo.CreateCertificateIntent(ctx, CertificateIntent{
				ApplicationID: app.ID, Domain: tc.domain, Enabled: false, Challenge: string(proxy.ChallengeHTTP01),
			}); err != nil {
				t.Fatalf("seed operator intent: %v", err)
			}
			if _, err := svc.AddDomain(svcCtx, userID, app.ID, DomainInput{Domain: tc.domain, Port: 3000, Protocol: ProtocolHTTPS}); err != nil {
				t.Fatalf("attach with a pre-existing intent: %v", err)
			}
			intents, err := repo.ListCertificateIntents(ctx, app.ID)
			if err != nil {
				t.Fatalf("list intents: %v", err)
			}
			var found bool
			for _, intent := range intents {
				if intent.Domain == tc.domain {
					found = true
					if intent.Enabled {
						t.Fatalf("operator intent was overwritten: %#v", intent)
					}
				}
			}
			if !found {
				t.Fatalf("operator intent vanished: %#v", intents)
			}
		})
	}
}
