package webhooks

import (
	"context"
	"net/http"
	"testing"

	"github.com/justindeelux/gotham/internal/providers"
)

// fakeAppEvents implements AppEventHandler without network access.
type fakeAppEvents struct {
	verified bool
	handled  []string
}

func (f *fakeAppEvents) VerifyDelivery(_ http.Header, _ []byte) bool { return f.verified }

func (f *fakeAppEvents) HandleAppEvent(_ context.Context, event string, _ []byte) error {
	f.handled = append(f.handled, event)
	return nil
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
