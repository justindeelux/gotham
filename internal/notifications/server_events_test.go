package notifications

import (
	"context"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

func TestServerEventsOnlyReachSubscribedGlobalChannels(t *testing.T) {
	repo := newFakeRepository()
	subscribed := sampleChannel(t)
	subscribed.Events = []EventKey{EventServerOffline, EventServerOnline}
	subscribed.ResourceType, subscribed.ResourceID = "", uuid.Nil
	subscribed = repo.seed(subscribed)
	scoped := subscribed
	scoped.ID, scoped.ResourceType, scoped.ResourceID = uuid.New(), ResourceApplication, uuid.New()
	scoped = repo.seed(scoped)
	legacy := subscribed
	legacy.ID, legacy.Events = uuid.New(), append([]EventKey(nil), defaultEvents...)
	legacy = repo.seed(legacy)
	service := newTestService(t, repo)
	globalSink, scopedSink, legacySink := newFakeNotifier(), newFakeNotifier(), newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{
		subscribed.ID: globalSink, scoped.ID: scopedSink, legacy.ID: legacySink,
	})
	for _, online := range []bool{false, true} {
		service.ServerStateChanged(context.Background(), servers.StateChange{Name: "edge", Host: "192.0.2.1", Online: online})
	}
	waitFor(t, "both global channel deliveries", func() bool { return len(globalSink.delivered()) == 2 })
	if len(scopedSink.delivered()) != 0 || len(legacySink.delivered()) != 0 {
		t.Fatal("server events reached a scoped or unsubscribed channel")
	}
	events := globalSink.delivered()
	keys := []EventKey{events[0].Key(), events[1].Key()}
	if !slices.Contains(keys, EventServerOffline) || !slices.Contains(keys, EventServerOnline) {
		t.Fatalf("keys: %v", keys)
	}
	for _, event := range events {
		if event.ResourceType != "" || event.ResourceID != uuid.Nil {
			t.Fatalf("event has resource scope: %+v", event)
		}
		if !strings.Contains(event.Message(), "edge") || !strings.Contains(event.Message(), "Host: 192.0.2.1") {
			t.Fatalf("message: %s", event.Message())
		}
	}
	if events[0].Summary() == events[1].Summary() {
		t.Fatal("online and offline summaries must differ")
	}
}

func TestServerEventInboxIsOwnerOnly(t *testing.T) {
	base := os.Getenv("GOTHAM_TEST_DSN")
	if base == "" {
		t.Skip("GOTHAM_TEST_DSN is required for this integration test")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	name := "notify_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)") }()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatal(err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	st := store.New(pool)
	owner, err := st.CreateFirstUser(ctx, "owner@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, "member@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(Config{Store: st, Secret: testSecret, Logger: discardLogger()})
	defer func() { _ = service.Close() }()
	for _, online := range []bool{false, true} {
		service.ServerStateChanged(ctx, servers.StateChange{Name: "edge", Online: online, At: time.Now()})
	}
	waitFor(t, "owner inbox", func() bool {
		page, err := service.ListNotifications(ctx, owner.ID.Bytes, 30)
		return err == nil && len(page.Notifications) == 2
	})
	page, err := service.ListNotifications(ctx, member.ID.Bytes, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notifications) != 0 || page.UnreadCount != 0 {
		t.Fatalf("member sees instance events: %+v", page)
	}
	page, err = service.ListNotifications(ctx, owner.ID.Bytes, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Notifications {
		if item.ProjectID != "" || item.ResourceID != "" || item.ResourceType != "" {
			t.Fatalf("instance event has scope: %+v", item)
		}
		if item.EventKey != EventServerOffline && item.EventKey != EventServerOnline {
			t.Fatalf("unexpected key: %s", item.EventKey)
		}
	}
}
