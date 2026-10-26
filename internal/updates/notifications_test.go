package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type availabilityRecorder struct {
	events   chan AvailableUpdate
	callback func(context.Context)
}

func (r availabilityRecorder) UpdateAvailable(ctx context.Context, update AvailableUpdate) {
	if r.callback != nil {
		r.callback(ctx)
	}
	r.events <- update
}

func TestUpdateAvailabilityDeduplication(t *testing.T) {
	version, status := "v1.2.0", 0
	var stateMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		tag, code := version, status
		stateMu.Unlock()
		releasesHandler([]fixtureRelease{{Tag: tag, Assets: platformAssets()}}, code, 0)(w, r)
	}))
	defer server.Close()
	events := make(chan AvailableUpdate, 10)
	svc := newTestService(t, server, "v1.0.0", func(cfg *Config) { cfg.Notifier = availabilityRecorder{events: events} }).(*service)
	for _, step := range []struct {
		version string
		channel Channel
		status  int
		want    bool
	}{
		{version: "v1.2.0", channel: ChannelStable, want: true},
		{version: "v1.2.0", channel: ChannelStable},
		{version: "v1.2.0", channel: ChannelStable, status: http.StatusBadGateway},
		{version: "v1.2.0", channel: ChannelStable},
		{version: "v1.0.0", channel: ChannelStable},
		{version: "v1.3.0", channel: ChannelStable, want: true},
		{version: "v1.2.0", channel: ChannelStable},
		{version: "v1.2.0", channel: ChannelBeta, want: true},
		{version: "v1.2.0", channel: ChannelBeta},
	} {
		stateMu.Lock()
		version, status = step.version, step.status
		stateMu.Unlock()
		release, err := svc.checkOn(context.Background(), step.channel)
		if (err != nil) != (status != 0) {
			t.Fatalf("check: release=%+v error=%v", release, err)
		}
		if step.want {
			select {
			case event := <-events:
				if event.Version != step.version || event.Channel != step.channel || event.At.IsZero() {
					t.Fatalf("event: %+v", event)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("availability notification did not arrive")
			}
		} else {
			select {
			case event := <-events:
				t.Fatalf("unexpected notification: %+v", event)
			case <-time.After(30 * time.Millisecond):
			}
		}
	}
	if len(events) != 0 {
		t.Fatalf("unexpected repeated notifications: %d", len(events))
	}
}

func TestUpdateNotifierCannotBlockCheck(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{{Tag: "v1.2.0", Assets: platformAssets()}}, 0, 0))
	defer server.Close()
	started, releaseNotifier := make(chan struct{}), make(chan struct{})
	defer close(releaseNotifier)
	events := make(chan AvailableUpdate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifier := availabilityRecorder{events: events, callback: func(notifyCtx context.Context) {
		cancel()
		deadline, ok := notifyCtx.Deadline()
		if notifyCtx.Err() != nil || !ok || time.Until(deadline) > 5*time.Second {
			t.Error("notifier context must be detached and bounded")
		}
		close(started)
		<-releaseNotifier
	}}
	svc := newTestService(t, server, "v1.0.0", func(cfg *Config) { cfg.Notifier = notifier })
	checked := make(chan error, 1)
	go func() { _, err := svc.Check(ctx); checked <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("notifier was not invoked")
	}
	select {
	case err := <-checked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("notification delayed a successful check")
	}
}

func TestConcurrentUpdateChecksNotifyOnce(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{{Tag: "v1.2.0", Assets: platformAssets()}}, 0, 0))
	defer server.Close()
	events := make(chan AvailableUpdate, 10)
	svc := newTestService(t, server, "v1.0.0", func(cfg *Config) { cfg.Notifier = availabilityRecorder{events: events} })
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := svc.Check(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("notification did not arrive")
	}
	select {
	case event := <-events:
		t.Fatalf("concurrent checks produced a duplicate: %+v", event)
	case <-time.After(30 * time.Millisecond):
	}
}
