package notifications

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/updates"
)

func TestUpdateEventOnlyReachesSubscribedGlobalChannels(t *testing.T) {
	repo := newFakeRepository()
	global := sampleChannel(t)
	global.ResourceType, global.ResourceID, global.Events = "", uuid.Nil, []EventKey{EventUpdateAvailable}
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
	at := time.Now().UTC()
	service.UpdateAvailable(context.Background(), updates.AvailableUpdate{Version: "v1.2.0", Channel: updates.ChannelStable, At: at})
	waitFor(t, "update channel delivery", func() bool { return len(globalSink.delivered()) == 1 })
	if len(scopedSink.delivered()) != 0 || len(legacySink.delivered()) != 0 || len(databaseSink.delivered()) != 0 {
		t.Fatal("update reached a scoped or unsubscribed channel")
	}
	event := globalSink.delivered()[0]
	if event.Key() != EventUpdateAvailable || !event.Key().Valid() {
		t.Fatalf("event key: %s", event.Key())
	}
	if event.ResourceType != "" || event.ResourceID != uuid.Nil {
		t.Fatalf("event has scope: %+v", event)
	}
	if event.Name != "v1.2.0 (stable)" || !event.At.Equal(at) || event.Error != "" {
		t.Fatalf("event: %+v", event)
	}
	if event.Summary() != "Gotham update available: v1.2.0 (stable)" || !strings.Contains(event.Message(), event.Summary()) {
		t.Fatalf("message: %s", event.Message())
	}
}

func TestUpdateEventInboxIsOwnerOnly(t *testing.T) {
	assertInstanceInboxVisibility(t, []EventKey{EventUpdateAvailable}, func(service *Service) {
		service.UpdateAvailable(context.Background(), updates.AvailableUpdate{Version: "v1.2.0", Channel: updates.ChannelStable})
	})
}
