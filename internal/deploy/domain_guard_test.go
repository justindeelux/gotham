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

	app := seedDomainApp(t, repo, user, "app.example.com")
	host := "PANEL.example.com"
	if _, err := svc.AddDomain(ctx, user, app.ID, DomainInput{Domain: host, Port: 3000}); !errors.Is(err, ErrValidation) {
		t.Fatalf("alias=%v", err)
	}

	if _, err := svc.CreatePreviewApplication(ctx, app.ID, PreviewApplicationInput{Name: "preview", Branch: "feat/test", BaseDomain: host}); !errors.Is(err, ErrValidation) {
		t.Fatalf("preview=%v", err)
	}
	alias, err := repo.CreateApplicationDomain(ctx, ApplicationDomain{ApplicationID: app.ID, Domain: "panel.example.com", Port: 3000, Protocol: ProtocolHTTP})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPrimaryDomain(ctx, user, app.ID, alias.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("primary=%v", err)
	}
	if err := repo.DeleteApplicationDomain(ctx, app.ID, alias.ID); err != nil {
		t.Fatal(err)
	}
	domains, err := svc.ListDomains(ctx, user, app.ID)
	if err != nil || len(domains) != 1 || domains[0].Domain != "app.example.com" {
		t.Fatalf("domains=%+v err=%v", domains, err)
	}
}
