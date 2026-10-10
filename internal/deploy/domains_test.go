package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// seedDomainApp stores one application with the given mirror domain in the
// fake, like the production create path would (row plus primary domain row).
func seedDomainApp(t *testing.T, repo *fakeRepository, userID uuid.UUID, domain string) Application {
	t.Helper()
	app := testApplication(userID)
	app.ID = uuid.New()
	app.ServerID = uuid.New()
	app.BaseDomain = domain
	repo.apps = append(repo.apps, app)
	if domain != "" {
		if _, err := repo.CreateApplicationDomain(context.Background(), ApplicationDomain{
			ApplicationID: app.ID,
			Domain:        domain,
			IsPrimary:     true,
		}); err != nil {
			t.Fatalf("seed primary domain: %v", err)
		}
	}
	return app
}

func TestAddDomainAttachesAlias(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	created, err := svc.AddDomain(context.Background(), userID, app.ID, "WWW.Example.com")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if created.Domain != "www.example.com" || created.IsPrimary {
		t.Fatalf("created = %#v, want the normalized alias", created)
	}
	domains, err := svc.ListDomains(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) != 2 || !domains[0].IsPrimary || domains[0].Domain != "app.example.com" {
		t.Fatalf("domains = %#v, want primary first", domains)
	}
}

func TestAddDomainFirstBecomesPrimary(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "")
	svc := newTestService(t, repo)

	created, err := svc.AddDomain(context.Background(), userID, app.ID, "app.example.com")
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if !created.IsPrimary {
		t.Fatalf("created = %#v, want the first domain primary", created)
	}
	stored, err := repo.GetApplication(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("get application: %v", err)
	}
	if stored.BaseDomain != "app.example.com" {
		t.Fatalf("mirror = %q, want the primary", stored.BaseDomain)
	}
}

func TestAddDomainRejectsConflicts(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	other := seedDomainApp(t, repo, userID, "other.example.com")
	svc := newTestService(t, repo)

	if _, err := svc.AddDomain(context.Background(), userID, app.ID, "not a domain"); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, "APP.example.com"); !errors.Is(err, ErrValidation) {
		t.Fatalf("same-app duplicate err = %v, want ErrValidation", err)
	}
	if _, err := svc.AddDomain(context.Background(), userID, app.ID, other.BaseDomain); !errors.Is(err, ErrDomainConflict) {
		t.Fatalf("cross-app duplicate err = %v, want ErrDomainConflict", err)
	}
}

func TestRemoveDomainPromotesOldest(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	first, err := svc.AddDomain(context.Background(), userID, app.ID, "b.example.com")
	if err != nil {
		t.Fatalf("add b: %v", err)
	}
	second, err := svc.AddDomain(context.Background(), userID, app.ID, "a.example.com")
	if err != nil {
		t.Fatalf("add a: %v", err)
	}
	_ = first
	primaryID := func() uuid.UUID {
		domains, err := svc.ListDomains(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, domain := range domains {
			if domain.IsPrimary {
				return domain.ID
			}
		}
		t.Fatal("no primary domain")
		return uuid.Nil
	}()

	updated, err := svc.RemoveDomain(context.Background(), userID, app.ID, primaryID)
	if err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	// b was added before a, so b is promoted deterministically.
	if updated.BaseDomain != "b.example.com" {
		t.Fatalf("mirror = %q, want the oldest remaining domain promoted", updated.BaseDomain)
	}
	domains, _ := svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 2 {
		t.Fatalf("domains = %#v, want two survivors", domains)
	}
	if !domains[0].IsPrimary || domains[0].Domain != "b.example.com" {
		t.Fatalf("domains = %#v, want b promoted", domains)
	}

	if _, err := svc.RemoveDomain(context.Background(), userID, app.ID, second.ID); err != nil {
		t.Fatalf("remove alias: %v", err)
	}
	domains, _ = svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 1 || domains[0].Domain != "b.example.com" {
		t.Fatalf("domains = %#v, want only b left", domains)
	}

	if _, err := svc.RemoveDomain(context.Background(), userID, app.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestRemoveLastDomainClearsMirror(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "only.example.com")
	svc := newTestService(t, repo)

	domains, err := svc.ListDomains(context.Background(), userID, app.ID)
	if err != nil || len(domains) != 1 {
		t.Fatalf("domains = %#v (%v), want the seeded primary", domains, err)
	}
	updated, err := svc.RemoveDomain(context.Background(), userID, app.ID, domains[0].ID)
	if err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	if updated.BaseDomain != "" {
		t.Fatalf("mirror = %q, want cleared", updated.BaseDomain)
	}
	domains, _ = svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
}

func TestSetPrimaryDomainFollowsMirror(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	alias, err := svc.AddDomain(context.Background(), userID, app.ID, "www.example.com")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	updated, err := svc.SetPrimaryDomain(context.Background(), userID, app.ID, alias.ID)
	if err != nil {
		t.Fatalf("SetPrimaryDomain: %v", err)
	}
	if updated.BaseDomain != "www.example.com" {
		t.Fatalf("mirror = %q, want the promoted alias", updated.BaseDomain)
	}
	domains, _ := svc.ListDomains(context.Background(), userID, app.ID)
	primaries := 0
	for _, domain := range domains {
		if domain.IsPrimary {
			primaries++
			if domain.Domain != "www.example.com" {
				t.Fatalf("primary = %#v", domain)
			}
		}
	}
	if primaries != 1 {
		t.Fatalf("domains = %#v, want exactly one primary", domains)
	}
	if _, err := svc.SetPrimaryDomain(context.Background(), userID, app.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestUpdateApplicationReconcilesPrimaryRow(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	app := seedDomainApp(t, repo, userID, "app.example.com")
	svc := newTestService(t, repo)

	next := "fresh.example.com"
	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{BaseDomain: &next})
	if err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}
	if updated.BaseDomain != next {
		t.Fatalf("mirror = %q", updated.BaseDomain)
	}
	domains, err := svc.ListDomains(context.Background(), userID, app.ID)
	if err != nil || len(domains) != 1 || domains[0].Domain != next || !domains[0].IsPrimary {
		t.Fatalf("domains = %#v (%v), want the re-targeted primary", domains, err)
	}

	cleared := ""
	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{BaseDomain: &cleared}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	domains, _ = svc.ListDomains(context.Background(), userID, app.ID)
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want cleared", domains)
	}
}
