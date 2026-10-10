package deploy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

// failUpdateRepo decorates a Repository to fail application-row writes on
// demand, proving multi-step domain mutations roll back instead of drifting.
type failUpdateRepo struct {
	Repository
	failUpdate bool
}

func (r *failUpdateRepo) UpdateApplication(ctx context.Context, app Application) (Application, error) {
	if r.failUpdate {
		return Application{}, errors.New("deploy: injected mirror failure")
	}
	return r.Repository.UpdateApplication(ctx, app)
}

func (r *failUpdateRepo) InTx(ctx context.Context, fn func(tx Repository) error) error {
	return r.Repository.InTx(ctx, func(tx Repository) error {
		return fn(&failUpdateRepo{Repository: tx, failUpdate: r.failUpdate})
	})
}

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
	userRow, err := st.CreateUser(ctx, fmt.Sprintf("p89-domains-%d@example.com", time.Now().UnixNano()), nil)
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
		BaseDomain:    domain,
		Port:          3000,
		ServerID:      serverID,
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
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

// TestDomainPromotionFlipFlopRealDB is the B1 regression: promote, flip back
// and promote again on a real database. The old single-statement conditional
// UPDATE failed the flip-back with 23505 depending on row visit order; the
// ordered demote-then-promote transaction must keep exactly one primary and
// a matching mirror on every flip.
func TestDomainPromotionFlipFlopRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)

	app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "flip", "a.example.com")
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, "b.example.com"); err != nil {
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
		updated, err := svc.SetPrimaryDomain(svcCtx, userID, app.ID, target)
		if err != nil {
			t.Fatalf("set primary %s: %v", want, err)
		}
		if updated.BaseDomain != want {
			t.Fatalf("mirror = %q, want %q", updated.BaseDomain, want)
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

// TestDomainMutationsRollBackRealDB proves every multi-step domain mutation
// is one transaction: when the mirror write fails, the domain rows are
// unchanged (no drift between rows and base_domain).
func TestDomainMutationsRollBackRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)
	repo := newStoreRepository(st, "integration-secret")
	// Fail only the mirror writes: every mutation below must then roll back
	// its domain-row writes instead of drifting.
	svc.repo = &failUpdateRepo{Repository: repo, failUpdate: true}

	domainRows := func(appID uuid.UUID) []ApplicationDomain {
		t.Helper()
		rows, err := repo.ListApplicationDomains(ctx, appID)
		if err != nil {
			t.Fatalf("list domains: %v", err)
		}
		return rows
	}

	app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "atomic", "a.example.com")
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, "b.example.com"); err != nil {
		t.Fatalf("add alias: %v", err)
	}

	// Base-domain rewrite fails on the mirror write: the primary row must
	// still record the old host and no new row may exist.
	if _, err := svc.UpdateApplication(svcCtx, userID, app.ID, UpdateApplicationInput{BaseDomain: strptr("c.example.com")}); err == nil {
		t.Fatal("update with failing mirror succeeded")
	}
	if rows := domainRows(app.ID); len(rows) != 2 || primaryOf(t, rows).Domain != "a.example.com" {
		t.Fatalf("rows drifted: %#v", rows)
	}
	stored, err := repo.GetApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("get application: %v", err)
	}
	if stored.BaseDomain != "a.example.com" {
		t.Fatalf("mirror drifted: %q", stored.BaseDomain)
	}

	// Removing the primary fails on the mirror write: the row must survive
	// and stay primary.
	rows := domainRows(app.ID)
	if _, err := svc.RemoveDomain(svcCtx, userID, app.ID, primaryOf(t, rows).ID); err == nil {
		t.Fatal("remove with failing mirror succeeded")
	}
	if rows := domainRows(app.ID); len(rows) != 2 || primaryOf(t, rows).Domain != "a.example.com" {
		t.Fatalf("rows drifted: %#v", rows)
	}

	// Promoting the alias fails on the mirror write: exactly one primary
	// must remain, still the old one.
	var aliasID uuid.UUID
	for _, domain := range domainRows(app.ID) {
		if !domain.IsPrimary {
			aliasID = domain.ID
		}
	}
	if _, err := svc.SetPrimaryDomain(svcCtx, userID, app.ID, aliasID); err == nil {
		t.Fatal("promote with failing mirror succeeded")
	}
	if rows := domainRows(app.ID); len(rows) != 2 || primaryOf(t, rows).Domain != "a.example.com" {
		t.Fatalf("rows drifted: %#v", rows)
	}
}

// TestRemoveDomainPromotesEnabledRealDB proves removal promotes the oldest
// ENABLED row, and only falls back to a disabled row when none remain (the
// mirror then carries the disabled flag, keeping the conflict visible).
func TestRemoveDomainPromotesEnabledRealDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := openDomainIntegrationStore(t, ctx)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)
	svc, svcCtx, userID := domainIntegrationService(t, ctx, st, teamID)

	app := createDomainIntegrationApp(t, svcCtx, svc, userID, envID, serverID, "promote", "old.example.com")
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, "disabled.example.com"); err != nil {
		t.Fatalf("add loser: %v", err)
	}
	if _, err := svc.AddDomain(svcCtx, userID, app.ID, "new.example.com"); err != nil {
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
	}); err != nil {
		t.Fatalf("disable row: %v", err)
	}

	updated, err := svc.RemoveDomain(svcCtx, userID, app.ID, byName["old.example.com"].ID)
	if err != nil {
		t.Fatalf("remove primary: %v", err)
	}
	if updated.BaseDomain != "new.example.com" || updated.BaseDomainDisabled {
		t.Fatalf("mirror = %q disabled=%v, want the enabled row promoted", updated.BaseDomain, updated.BaseDomainDisabled)
	}

	rows, err = svc.ListDomains(svcCtx, userID, app.ID)
	if err != nil {
		t.Fatalf("list domains: %v", err)
	}
	byName = make(map[string]ApplicationDomain)
	for _, row := range rows {
		byName[row.Domain] = row
	}
	updated, err = svc.RemoveDomain(svcCtx, userID, app.ID, byName["new.example.com"].ID)
	if err != nil {
		t.Fatalf("remove promoted: %v", err)
	}
	if updated.BaseDomain != "disabled.example.com" || !updated.BaseDomainDisabled {
		t.Fatalf("mirror = %q disabled=%v, want the disabled fallback carried visibly", updated.BaseDomain, updated.BaseDomainDisabled)
	}
}

func strptr(value string) *string { return &value }
