package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/justindeelux/gotham/internal/proxy"
)

// TestUpdateApplicationNormalizesLegacyMixedCaseDomain proves an unrelated
// update (rename) of an application whose stored domain predates
// normalization succeeds and rewrites the value lowercase, even with the
// proxy feature disabled (BE-6.1 F8).
func TestUpdateApplicationNormalizesLegacyMixedCaseDomain(t *testing.T) {
	t.Setenv(proxy.FeatureEnv, "false")

	userID := uuid.New()
	app := testApplication(userID)
	app.BaseDomain = "App.Example.com" // legacy stored casing

	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	name := "renamed"
	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{Name: &name})
	if err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}
	if updated.BaseDomain != "app.example.com" {
		t.Fatalf("domain = %q, want normalized %q", updated.BaseDomain, "app.example.com")
	}
	if updated.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", updated.Name)
	}
}

// TestUpdateApplicationClearsDisabledDomainOnExplicitChange proves an explicit
// domain update re-enables a binding the uniqueness migration had disabled.
func TestUpdateApplicationClearsDisabledDomainOnExplicitChange(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.BaseDomain = "legacy.example.com"
	app.BaseDomainDisabled = true

	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	domain := "Fresh.Example.com"
	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{BaseDomain: &domain})
	if err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}
	if updated.BaseDomainDisabled {
		t.Fatal("explicit domain update left the binding disabled")
	}
	if updated.BaseDomain != "fresh.example.com" {
		t.Fatalf("domain = %q, want normalized", updated.BaseDomain)
	}

	// An unrelated update keeps the disabled mark until the owner changes the
	// domain.
	app.BaseDomainDisabled = true
	repo.app = app
	name := "still disabled"
	kept, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{Name: &name})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !kept.BaseDomainDisabled {
		t.Fatal("rename cleared the disabled binding")
	}

	// A full-form update that resends the unchanged domain must not
	// reactivate a migration-disabled binding either (BE-6.1 F6).
	app.BaseDomainDisabled = true
	repo.app = app
	same := "legacy.example.com"
	fullForm := "full form rename"
	kept, err = svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{
		Name:       &fullForm,
		BaseDomain: &same,
	})
	if err != nil {
		t.Fatalf("full-form update: %v", err)
	}
	if !kept.BaseDomainDisabled {
		t.Fatal("unchanged full-form domain update cleared the disabled binding")
	}
	if kept.BaseDomain != same {
		t.Fatalf("domain = %q, want unchanged %q", kept.BaseDomain, same)
	}
}

// TestUpdateApplicationPreservesDisabledLegacyMixedCaseDomain proves a
// full-form resend of a legacy mixed-case disabled domain normalizes the
// stored value without reactivating the binding.
func TestUpdateApplicationPreservesDisabledLegacyMixedCaseDomain(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.BaseDomain = "Legacy.Example.com"
	app.BaseDomainDisabled = true

	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	same := "Legacy.Example.com"
	name := "renamed"
	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{
		Name:       &name,
		BaseDomain: &same,
	})
	if err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}
	if !updated.BaseDomainDisabled {
		t.Fatal("unchanged legacy value reactivated the disabled binding")
	}
	if updated.BaseDomain != "legacy.example.com" {
		t.Fatalf("domain = %q, want normalized legacy value", updated.BaseDomain)
	}
}

// TestApplicationWriteErrorMapsDomainConflict proves the new per-node domain
// index surfaces as ErrDomainConflict while the existing name index keeps its
// validation mapping (BE-6.1 F6, PE-2 R5).
func TestApplicationWriteErrorMapsDomainConflict(t *testing.T) {
	domainErr := applicationWriteError(&pgconn.PgError{
		Code:           "23505",
		ConstraintName: "applications_server_domain_idx",
	}, "demo")
	if !errors.Is(domainErr, ErrDomainConflict) {
		t.Fatalf("domain conflict err = %v, want ErrDomainConflict", domainErr)
	}

	nameErr := applicationWriteError(&pgconn.PgError{
		Code:           "23505",
		ConstraintName: "applications_user_name_idx",
	}, "demo")
	if !errors.Is(nameErr, ErrValidation) {
		t.Fatalf("name conflict err = %v, want ErrValidation", nameErr)
	}
}
