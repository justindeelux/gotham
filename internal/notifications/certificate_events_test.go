package notifications

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/proxy"
)

func TestCertificateEventOnlyReachesSubscribedGlobalChannels(t *testing.T) {
	repo := newFakeRepository()
	global := sampleChannel(t)
	global.ResourceType, global.ResourceID, global.Events = "", uuid.Nil, []EventKey{EventCertificateExpiring}
	global = repo.seed(global)
	scoped := global
	scoped.ID, scoped.ResourceType, scoped.ResourceID = uuid.New(), ResourceApplication, uuid.New()
	scoped = repo.seed(scoped)
	database := scoped
	database.ID, database.ResourceType = uuid.New(), ResourceDatabase
	database = repo.seed(database)
	legacy := global
	legacy.ID, legacy.Events = uuid.New(), append([]EventKey(nil), defaultEvents...)
	legacy = repo.seed(legacy)
	service := newTestService(t, repo)
	globalSink, scopedSink, legacySink, databaseSink := newFakeNotifier(), newFakeNotifier(), newFakeNotifier(), newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{global.ID: globalSink, scoped.ID: scopedSink, legacy.ID: legacySink, database.ID: databaseSink})
	expiry := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	service.CertificateExpiring(context.Background(), proxy.CertificateExpiry{Domain: "panel.example.com", NotAfter: expiry})
	waitFor(t, "certificate channel delivery", func() bool { return len(globalSink.delivered()) == 1 })
	if len(scopedSink.delivered()) != 0 || len(legacySink.delivered()) != 0 || len(databaseSink.delivered()) != 0 {
		t.Fatal("certificate warning reached a scoped or unsubscribed channel")
	}
	event := globalSink.delivered()[0]
	if event.Key() != EventCertificateExpiring || !event.Key().Valid() {
		t.Fatalf("event key: %s", event.Key())
	}
	if event.ResourceType != "" || event.ResourceID != uuid.Nil {
		t.Fatalf("event has scope: %+v", event)
	}
	if event.Name != "panel.example.com" || event.Host != event.Name || !strings.Contains(event.Error, expiry.Format(time.RFC3339)) {
		t.Fatalf("event: %+v", event)
	}
	if event.Summary() != "Certificate expires soon: panel.example.com" || !strings.Contains(event.Message(), event.Error) {
		t.Fatalf("message: %s", event.Message())
	}
}

func TestCertificateEventInboxIsOwnerOnly(t *testing.T) {
	assertInstanceInboxVisibility(t, []EventKey{EventCertificateExpiring}, func(service *Service) {
		service.CertificateExpiring(context.Background(), proxy.CertificateExpiry{
			Domain: "panel.example.com", NotAfter: time.Now().Add(24 * time.Hour),
		})
	})
}
