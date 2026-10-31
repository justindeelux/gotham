package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/cleanup"
)

func TestCleanupFinishedMapsOutcomeAndDetail(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := NewService(Config{
		Repository:  repo,
		Secret:      testSecret,
		Logger:      discardLogger(),
		Mailer:      &fakeMailer{},
		Workers:     1,
		QueueSize:   8,
		SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.CleanupFinished(context.Background(), cleanup.ScheduleRunSummary{
		ScheduleID:   uuid.New(),
		ScheduleName: "nightly",
		Nodes: []cleanup.ScheduleNodeResult{
			{ServerName: "alpha", Status: cleanup.StatusOK, ReclaimedBytes: 2048},
			{ServerName: "beta", Status: cleanup.StatusFailed, Reason: "agent exited"},
		},
	})
	waitFor(t, "the cleanup failure event", func() bool { return len(notifier.delivered()) == 1 })
	event := notifier.delivered()[0]
	if event.Key() != EventCleanupFailure {
		t.Fatalf("key = %q, want cleanup_failure", event.Key())
	}
	if event.Name != "nightly" || event.Error != "beta: agent exited" {
		t.Fatalf("event = %+v, want the schedule name and the failed node", event)
	}
	if event.Detail != "Freed: 2.0 KB on alpha, 0 B on beta" {
		t.Fatalf("detail = %q", event.Detail)
	}

	service.CleanupFinished(context.Background(), cleanup.ScheduleRunSummary{
		ScheduleName: "nightly",
		Nodes: []cleanup.ScheduleNodeResult{
			{ServerName: "alpha", Status: cleanup.StatusOK, ReclaimedBytes: 2048},
		},
	})
	waitFor(t, "the cleanup success event", func() bool { return len(notifier.delivered()) == 2 })
	success := notifier.delivered()[1]
	if success.Key() != EventCleanupSuccess || success.Error != "" {
		t.Fatalf("success = %+v, want a clean cleanup_success", success)
	}
}

func TestCleanupEventsAreSubscribable(t *testing.T) {
	for _, key := range []EventKey{EventCleanupSuccess, EventCleanupFailure} {
		if !key.Valid() {
			t.Fatalf("%q is not a valid channel event key", key)
		}
	}
}
