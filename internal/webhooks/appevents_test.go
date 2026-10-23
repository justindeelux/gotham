package webhooks

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

type fakeAppEvents struct {
	verified bool
	appID    uuid.UUID
	handled  []string
}

func (f *fakeAppEvents) VerifyDelivery(_ http.Header, _ []byte) (uuid.UUID, bool) {
	return f.appID, f.verified
}

func (f *fakeAppEvents) HandleAppEvent(_ context.Context, _ uuid.UUID, event string, _ []byte) error {
	f.handled = append(f.handled, event)
	return nil
}

type fakeAppPush struct {
	verified bool
	appID    uuid.UUID
	targets  []AppPushTarget
}

func (f *fakeAppPush) VerifyPush(_ http.Header, _ []byte) (uuid.UUID, bool) {
	return f.appID, f.verified
}

func (f *fakeAppPush) PushTargets(_ context.Context, _ uuid.UUID, _ string) ([]AppPushTarget, error) {
	return f.targets, nil
}

func appEventRequest(event, body string) *http.Request {
	return deliveryRequest(providers.NameGitHub, body, map[string]string{
		headerGitHubEvent:    event,
		headerGitHubDelivery: "delivery-1",
	}, "203.0.113.7:1234")
}

func TestReceiveAppEventRefreshesCache(t *testing.T) {
	events := &fakeAppEvents{verified: true}
	svc := newTestServiceWith(Config{
		Repository:  newFakeRepository(),
		Installer:   &fakeInstaller{},
		Deployer:    &fakeDeployer{},
		Provisioner: &fakeDeployer{},
		AppEvents:   events,
	})

	body := `{"action":"added","installation":{"id":999}}`
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub, appEventRequest("installation_repositories", body))
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != StatusIgnored {
		t.Fatalf("status = %q, want ignored", delivery.Status)
	}
	if len(events.handled) != 1 || events.handled[0] != "installation_repositories" {
		t.Fatalf("handled = %v", events.handled)
	}
}

func TestReceiveAppEventRejectsUnverified(t *testing.T) {
	events := &fakeAppEvents{verified: false}
	svc := newTestServiceWith(Config{
		Repository:  newFakeRepository(),
		Installer:   &fakeInstaller{},
		Deployer:    &fakeDeployer{},
		Provisioner: &fakeDeployer{},
		AppEvents:   events,
	})

	body := `{"action":"added","installation":{"id":999}}`
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, appEventRequest("installation", body)); err == nil {
		t.Fatal("unverified installation event was accepted")
	}
	if len(events.handled) != 0 {
		t.Fatalf("handled = %v", events.handled)
	}
}

func TestReceiveAppEventWithoutHandler(t *testing.T) {
	svc := newTestService(newFakeRepository(), &fakeInstaller{}, &fakeDeployer{})

	body := `{"action":"added","installation":{"id":999}}`
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, appEventRequest("installation", body)); err == nil {
		t.Fatal("installation event without handler was accepted")
	}
}

func appPushBody() string {
	return `{"ref":"refs/heads/main","after":"abc123def456",` +
		`"repository":{"full_name":"acme/web"},` +
		`"installation":{"id":999,"account":{"login":"acme"}}}`
}

func TestAppPushTriggersDeploy(t *testing.T) {
	repo := newFakeRepository()
	repo.app.Repo = "acme/web"
	repo.app.Branch = "main"
	deployer := &fakeDeployer{}
	push := &fakeAppPush{verified: true, appID: uuid.New(), targets: []AppPushTarget{
		{ApplicationID: repo.app.ID, Branch: "main"},
	}}
	svc := newTestServiceWith(Config{
		Repository:  repo,
		Installer:   &fakeInstaller{},
		Deployer:    deployer,
		Provisioner: &fakeDeployer{},
		AppPush:     push,
	})

	req := deliveryRequest(providers.NameGitHub, appPushBody(), map[string]string{
		headerGitHubEvent:    "push",
		headerGitHubDelivery: "delivery-app-1",
	}, "203.0.113.7:1234")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub, req)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != StatusQueued {
		t.Fatalf("status = %q, want queued", delivery.Status)
	}
	deployer.mu.Lock()
	defer deployer.mu.Unlock()
	if len(deployer.deployed) != 1 || deployer.deployed[0] != repo.app.ID {
		t.Fatalf("deployed = %v, want [%v]", deployer.deployed, repo.app.ID)
	}
}

func TestAppPushWithoutHandler(t *testing.T) {
	svc := newTestService(newFakeRepository(), &fakeInstaller{}, &fakeDeployer{})

	req := deliveryRequest(providers.NameGitHub, appPushBody(), map[string]string{
		headerGitHubEvent:    "push",
		headerGitHubDelivery: "delivery-app-2",
	}, "203.0.113.7:1234")
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, req); err == nil {
		t.Fatal("app-signed push without handler was accepted")
	}
}

func TestAppPushWrongBranch(t *testing.T) {
	repo := newFakeRepository()
	repo.app.Repo = "acme/web"
	repo.app.Branch = "main"
	deployer := &fakeDeployer{}
	push := &fakeAppPush{verified: true, appID: uuid.New(), targets: []AppPushTarget{
		{ApplicationID: repo.app.ID, Branch: "main"},
	}}
	svc := newTestServiceWith(Config{
		Repository:  repo,
		Installer:   &fakeInstaller{},
		Deployer:    deployer,
		Provisioner: &fakeDeployer{},
		AppPush:     push,
	})

	body := `{"ref":"refs/heads/other","after":"abc123def456",` +
		`"repository":{"full_name":"acme/web"},` +
		`"installation":{"id":999}}`
	req := deliveryRequest(providers.NameGitHub, body, map[string]string{
		headerGitHubEvent:    "push",
		headerGitHubDelivery: "delivery-app-3",
	}, "203.0.113.7:1234")
	delivery, err := svc.Receive(context.Background(), providers.NameGitHub, req)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != StatusIgnored {
		t.Fatalf("status = %q, want ignored", delivery.Status)
	}
}
