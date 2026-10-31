package cleanup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

func thresholdSettings() store.CleanupThreshold {
	return store.CleanupThreshold{
		Enabled: true, Percent: 85, Categories: []string{CategoryBuildCache},
		MinAgeHours: 24, CooldownHours: 6,
	}
}

func thresholdObservation(serverID uuid.UUID, usage float64) servers.NodeObservation {
	return servers.NodeObservation{
		ServerID: serverID,
		Host:     servers.HostObservation{DiskUsage: servers.ObservedMetric{Status: servers.MetricStatusOK, Value: usage}},
	}
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForThresholdIdle(t *testing.T, svc *Service, serverID uuid.UUID, want int, st *fakeStore) {
	t.Helper()
	waitFor(t, "the threshold run to finish", func() bool {
		return !svc.thresholdRunning(serverID) && len(st.recordedRuns()) == want
	})
}

func TestThresholdStartsOneRunAndHonoursCooldown(t *testing.T) {
	agent := buildCacheAgent(1024)
	st := &fakeStore{}
	center := &fakeCenter{threshold: thresholdSettings()}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, agent, st, center, notifier)
	clock := svc.now()

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitForThresholdIdle(t, svc, serverID, 1, st)

	runs := st.recordedRuns()
	if len(runs) != 1 || runs[0].Trigger != TriggerThreshold {
		t.Fatalf("runs = %+v, want one threshold run", runs)
	}

	// Stored history carries the cooldown, so a restarted service still waits.
	center.latestRun = &runs[0]
	svc2, _ := testScheduledService(t, agent, st, center, notifier)
	svc2.now = func() time.Time { return clock.Add(time.Hour) }
	svc2.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitFor(t, "no run inside the cooldown", func() bool { return len(st.recordedRuns()) == 1 })

	// Staying above after the cooldown starts exactly one more run.
	svc2.now = func() time.Time { return clock.Add(7 * time.Hour) }
	svc2.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitForThresholdIdle(t, svc2, serverID, 2, st)
}

func TestThresholdBelowPercentOrDisabledDoesNothing(t *testing.T) {
	for name, test := range map[string]struct {
		usage    float64
		settings store.CleanupThreshold
	}{
		"below":    {usage: 0.5, settings: thresholdSettings()},
		"disabled": {usage: 0.9, settings: store.CleanupThreshold{Enabled: false, Percent: 85, Categories: []string{CategoryBuildCache}}},
	} {
		t.Run(name, func(t *testing.T) {
			st := &fakeStore{}
			center := &fakeCenter{threshold: test.settings}
			svc, serverID := testScheduledService(t, buildCacheAgent(1024), st, center, &fakeNotifier{})
			svc.Observe(context.Background(), thresholdObservation(serverID, test.usage))
			time.Sleep(20 * time.Millisecond)
			if runs := st.recordedRuns(); len(runs) != 0 {
				t.Fatalf("runs = %+v, want none", runs)
			}
		})
	}
}

func TestThresholdStillAboveNotifiesOncePerRun(t *testing.T) {
	st := &fakeStore{}
	center := &fakeCenter{threshold: thresholdSettings()}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, buildCacheAgent(2048), st, center, notifier)
	clock := svc.now()

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitForThresholdIdle(t, svc, serverID, 1, st)
	runs := st.recordedRuns()
	center.latestRun = &runs[0]

	svc.now = func() time.Time { return clock.Add(10 * time.Minute) }
	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	notified := notifier.notified()
	if len(notified) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notified))
	}
	summary := notified[0]
	if !summary.Failed() || !strings.Contains(summary.Nodes[0].Reason, "still above 85% after cleanup, freed 2.0 KB") {
		t.Fatalf("summary = %+v, want the still-above failure", summary)
	}

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	if got := len(notifier.notified()); got != 1 {
		t.Fatalf("notifications = %d, want the run notified once", got)
	}
}

func TestThresholdNothingMatchedRecordsOnceAndCoolsDown(t *testing.T) {
	agent := buildCacheAgent(1024)
	st := &fakeStore{}
	settings := thresholdSettings()
	settings.Categories = []string{CategoryJournal}
	center := &fakeCenter{threshold: settings}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, agent, st, center, notifier)
	clock := svc.now()

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitForThresholdIdle(t, svc, serverID, 1, st)

	runs := st.recordedRuns()
	if len(runs) != 1 || runs[0].Status != StatusOK || runs[0].Trigger != TriggerThreshold ||
		runs[0].ReclaimedBytes != 0 {
		t.Fatalf("runs = %+v, want one empty threshold run", runs)
	}

	center.latestRun = &runs[0]
	svc.now = func() time.Time { return clock.Add(10 * time.Minute) }
	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	notified := notifier.notified()
	if len(notified) != 1 || !strings.Contains(notified[0].Nodes[0].Reason, "freed 0 B") {
		t.Fatalf("notifications = %+v, want one still-above notice", notified)
	}
	if calls := agent.scanCalls.Load(); calls != 1 {
		t.Fatalf("scans = %d, want no rescan inside the cool-down", calls)
	}

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	if got := len(notifier.notified()); got != 1 || agent.scanCalls.Load() != 1 {
		t.Fatalf("notifications = %d, scans = %d, want one of each", got, agent.scanCalls.Load())
	}
}

func TestThresholdExclusionFailureRecordsOneRun(t *testing.T) {
	st := &fakeStore{excludeErr: errors.New("exclusions unavailable")}
	center := &fakeCenter{threshold: thresholdSettings()}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, buildCacheAgent(1024), st, center, notifier)
	clock := svc.now()

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitForThresholdIdle(t, svc, serverID, 1, st)
	waitFor(t, "the failed run notification", func() bool { return len(notifier.notified()) == 1 })

	runs := st.recordedRuns()
	if len(runs) != 1 || runs[0].Status != StatusFailed ||
		!strings.Contains(runs[0].Reason, "exclusions unavailable") {
		t.Fatalf("runs = %+v, want one failed threshold run", runs)
	}

	center.latestRun = &runs[0]
	svc.now = func() time.Time { return clock.Add(10 * time.Minute) }
	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	if got := len(notifier.notified()); got != 1 || len(st.recordedRuns()) != 1 {
		t.Fatalf("notifications = %d, runs = %d, want no repeat inside the cool-down",
			got, len(st.recordedRuns()))
	}
}

func TestThresholdSkippedRunDoesNotClaimCleanup(t *testing.T) {
	st := &fakeStore{}
	center := &fakeCenter{threshold: thresholdSettings()}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, buildCacheAgent(1024), st, center, notifier)
	clock := svc.now()
	center.latestRun = &store.CleanupRun{
		ID: uuid.New(), ServerID: serverID, Trigger: TriggerThreshold,
		StartedAt: clock, FinishedAt: clock, Status: StatusSkipped, Reason: ReasonBusy,
	}

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	if got := len(notifier.notified()); got != 0 {
		t.Fatalf("notifications = %d, want none for a skipped run", got)
	}
	if runs := st.recordedRuns(); len(runs) != 0 {
		t.Fatalf("runs = %+v, want no new run inside the cool-down", runs)
	}
}

func TestThresholdFailedRunNotifies(t *testing.T) {
	agent := buildCacheAgent(1024)
	agent.cleanErr = status.Error(codes.Internal, "agent exploded")
	st := &fakeStore{}
	center := &fakeCenter{threshold: thresholdSettings()}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, agent, st, center, notifier)
	clock := svc.now()

	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	waitFor(t, "the failed run notification", func() bool { return len(notifier.notified()) == 1 })
	runs := st.recordedRuns()
	if len(runs) != 1 || runs[0].Status != StatusFailed {
		t.Fatalf("runs = %+v, want one failed threshold run", runs)
	}
	center.latestRun = &runs[0]
	svc.now = func() time.Time { return clock.Add(10 * time.Minute) }
	svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
	if got := len(notifier.notified()); got != 1 {
		t.Fatalf("notifications = %d, want no duplicate after a failed run", got)
	}
}

func TestThresholdObservationDoesNotBlockOnTheRun(t *testing.T) {
	agent := buildCacheAgent(1024)
	release := make(chan struct{})
	agent.scanBlock = release
	st := &fakeStore{}
	center := &fakeCenter{threshold: thresholdSettings()}
	svc, serverID := testScheduledService(t, agent, st, center, &fakeNotifier{})

	done := make(chan struct{})
	go func() {
		svc.Observe(context.Background(), thresholdObservation(serverID, 0.9))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe blocked on the run")
	}
	close(release)
	waitForThresholdIdle(t, svc, serverID, 1, st)
}

func TestUpdateThresholdValidates(t *testing.T) {
	svc, _ := testScheduledService(t, buildCacheAgent(1), &fakeStore{}, &fakeCenter{}, &fakeNotifier{})
	valid := ThresholdInput{
		Enabled: true, Percent: 85, Categories: []string{CategoryBuildCache},
		MinAgeHours: 24, CooldownHours: 6,
	}
	updated, err := svc.UpdateThreshold(context.Background(), uuid.Nil, valid)
	if err != nil || !updated.Enabled || updated.Percent != 85 {
		t.Fatalf("UpdateThreshold(valid) = %+v, %v", updated, err)
	}

	for name, test := range map[string]struct {
		input ThresholdInput
		want  error
	}{
		"dangerous": {input: withThreshold(valid, func(in *ThresholdInput) {
			in.Categories = []string{CategoryOrphanedVolumes}
		}), want: ErrDangerousCategory},
		"unknown": {input: withThreshold(valid, func(in *ThresholdInput) {
			in.Categories = []string{"nope"}
		}), want: ErrInvalidCategory},
		"percent": {input: withThreshold(valid, func(in *ThresholdInput) { in.Percent = 30 }), want: ErrValidation},
		"age": {input: withThreshold(valid, func(in *ThresholdInput) {
			in.MinAgeHours = 9000
		}), want: ErrValidation},
		"cooldown": {input: withThreshold(valid, func(in *ThresholdInput) {
			in.CooldownHours = 200
		}), want: ErrValidation},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.UpdateThreshold(context.Background(), uuid.Nil, test.input); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func withThreshold(input ThresholdInput, change func(*ThresholdInput)) ThresholdInput {
	change(&input)
	return input
}
