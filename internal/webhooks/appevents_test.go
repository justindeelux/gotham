package webhooks

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// fakeAppEvents implements AppEventHandler without network access.
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

// fakeAppPush implements AppPushHandler without network access.
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

// TestReceiveAppEventRefreshesCache proves installation deliveries verify
// through the app secret and refresh the cache without touching hooks.
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

// TestReceiveAppEventRejectsUnverified proves a bad app signature is refused.
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

// TestReceiveAppEventWithoutHandler proves installation events stay
// unauthorized when no app service is wired.
func TestReceiveAppEventWithoutHandler(t *testing.T) {
	svc := newTestService(newFakeRepository(), &fakeInstaller{}, &fakeDeployer{})

	body := `{"action":"added","installation":{"id":999}}`
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, appEventRequest("installation", body)); err == nil {
		t.Fatal("installation event without handler was accepted")
	}
}

// appPushBody is a push delivery signed with the app webhook secret.
func appPushBody() string {
	return `{"ref":"refs/heads/main","after":"abc123def456",` +
		`"repository":{"full_name":"acme/web"},` +
		`"installation":{"id":999,"account":{"login":"acme"}}}`
}

// TestAppPushTriggersDeploy proves a push signed with the app webhook secret
// deploys the matching github_app application even though no per-hook secret
// exists: connect -> install -> create app -> signed push -> queued.
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

// TestAppPushWithoutHandler proves an app-signed push stays unauthorized
// when no app push handler is wired (the hook path cannot verify it).
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

// TestAppPushWrongBranch proves an app push to another branch builds nothing.
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
