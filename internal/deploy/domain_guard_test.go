package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestApplicationCannotClaimPanelHost(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	svc.guardDomains = func(_ context.Context, hosts ...string) (func(), error) {
		if hosts[0] == "panel.example.com" {
			return nil, errors.New("reserved for the control plane")
		}
		return func() {}, nil
	}
	input := validCreateInput(uuid.New())
	input.BaseDomain = "PANEL.example.com"
	if _, err := svc.CreateApplication(ctx, user, input); !errors.Is(err, ErrValidation) {
		t.Fatalf("create=%v", err)
	}
	app := seedDomainApp(t, repo, user, "app.example.com")
	host := "PANEL.example.com"
	if _, err := svc.UpdateApplication(ctx, user, app.ID, UpdateApplicationInput{BaseDomain: &host}); !errors.Is(err, ErrValidation) {
		t.Fatalf("update=%v", err)
	}
	if _, err := svc.AddDomain(ctx, user, app.ID, host); !errors.Is(err, ErrValidation) {
		t.Fatalf("alias=%v", err)
	}

	if _, err := svc.CreatePreviewApplication(ctx, app.ID, PreviewApplicationInput{Name: "preview", Branch: "feat/test", BaseDomain: host}); !errors.Is(err, ErrValidation) {
		t.Fatalf("preview=%v", err)
	}
	// A legacy alias must not become the primary after the panel claims its host.
	alias, err := repo.CreateApplicationDomain(ctx, ApplicationDomain{ApplicationID: app.ID, Domain: "panel.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetPrimaryDomain(ctx, user, app.ID, alias.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("primary=%v", err)
	}
	if err := repo.DeleteApplicationDomain(ctx, app.ID, alias.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetApplication(ctx, app.ID)
	if err != nil || stored.BaseDomain != "app.example.com" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	domains, err := svc.ListDomains(ctx, user, app.ID)
	if err != nil || len(domains) != 1 {
		t.Fatalf("domains=%+v err=%v", domains, err)
	}
}
