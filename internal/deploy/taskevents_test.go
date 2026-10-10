package deploy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/taskevents"
)

// taskFramesOf decodes the task lifecycle frames a publisher captured.
func taskFramesOf(t *testing.T, pub *recordPublisher) []taskevents.Event {
	t.Helper()
	var out []taskevents.Event
	for _, event := range pub.payloads() {
		if event.Type != "task" {
			continue
		}
		var ev taskevents.Event
		if err := json.Unmarshal([]byte(event.Data), &ev); err != nil {
			t.Fatalf("decode task frame: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func statusesOf(events []taskevents.Event) []taskevents.Status {
	out := make([]taskevents.Status, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Status)
	}
	return out
}

// TestTaskEventsPublishedOnRun is the JUS-91 backend contract: a deploy run
// publishes running events with step/progress plus a terminal event on the
// team's task channel, and the tracker is empty once the run is terminal.
func TestTaskEventsPublishedOnRun(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	src := &fakeSource{logs: []string{"checking out " + app.Branch}}
	node := newMockNode()
	pub := &recordPublisher{}
	tracker := taskevents.NewTracker()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     src,
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
		Tasks:      tracker,
	})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	frames := taskFramesOf(t, pub)
	if len(frames) == 0 {
		t.Fatal("no task frames published")
	}
	for _, ev := range frames {
		if ev.TaskID != dep.ID.String() || ev.TeamID != app.TeamID.String() || ev.Name != app.Name {
			t.Errorf("unexpected task identity %+v", ev)
		}
	}
	got := statusesOf(frames)
	if got[len(got)-1] != taskevents.StatusSucceeded {
		t.Errorf("last status = %s, want succeeded", got[len(got)-1])
	}
	var sawBuilding bool
	for _, ev := range frames {
		if ev.Status == taskevents.StatusRunning && ev.Step == string(StateBuilding) {
			sawBuilding = true
		}
	}
	if !sawBuilding {
		t.Errorf("no running frame for the building step in %v", got)
	}
	if running := tracker.Snapshot(app.TeamID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after a terminal run", len(running))
	}
}

// TestSubmitPublishesQueued is the webhook-triggered contract: DeploySystem
// (the push-webhook path, no authenticated caller) shares submit with the
// UI-started Deploy, so both surface a queued card from the same boundary.
func TestSubmitPublishesQueued(t *testing.T) {
	for _, viaSystem := range []bool{false, true} {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		pub := &recordPublisher{}
		tracker := taskevents.NewTracker()
		svc := NewService(Config{
			Repository: repo,
			Secret:     testSecretKey,
			Logger:     discardLogger(),
			Publisher:  pub,
			Tasks:      tracker,
			// The worker parks here, so the queued assertion below cannot
			// race the run draining the tracker. Close releases it.
			Dial: func(ctx context.Context, _ uuid.UUID) (Node, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		})
		t.Cleanup(func() { _ = svc.Close() })

		var queued Deployment
		var err error
		if viaSystem {
			queued, err = svc.DeploySystem(context.Background(), app.ID)
		} else {
			queued, err = svc.Deploy(context.Background(), userID, app.ID)
		}
		if err != nil {
			t.Fatalf("viaSystem=%v: queue: %v", viaSystem, err)
		}
		var found *taskevents.Event
		for _, ev := range taskFramesOf(t, pub) {
			if ev.TaskID == queued.ID.String() && ev.Status == taskevents.StatusQueued {
				found = &ev
			}
		}
		if found == nil {
			t.Fatalf("viaSystem=%v: no queued frame for %s", viaSystem, queued.ID)
		}
		if found.Name != app.Name {
			t.Errorf("viaSystem=%v: name = %q, want %q", viaSystem, found.Name, app.Name)
		}
		if running := tracker.Snapshot(app.TeamID.String()); len(running) != 1 {
			t.Errorf("viaSystem=%v: tracker holds %d tasks, want 1", viaSystem, len(running))
		}
	}
}

// TestTaskEventsPublishedOnFailure covers the failed card path: a run that
// cannot reach its node publishes a failed event carrying the error.
func TestTaskEventsPublishedOnFailure(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	pub := &recordPublisher{}
	tracker := taskevents.NewTracker()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Dial:       func(context.Context, uuid.UUID) (Node, error) { return nil, ErrAgentUnavailable },
		Emitter:    NewEmitter(pub),
		Tasks:      tracker,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	frames := taskFramesOf(t, pub)
	if len(frames) == 0 {
		t.Fatal("no task frames published")
	}
	last := frames[len(frames)-1]
	if last.Status != taskevents.StatusFailed {
		t.Errorf("last status = %s, want failed", last.Status)
	}
	if strings.TrimSpace(last.Error) == "" {
		t.Error("failed frame carries no error text")
	}
	if running := tracker.Snapshot(app.TeamID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after a failed run", len(running))
	}
}
