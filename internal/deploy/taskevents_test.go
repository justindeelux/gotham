package deploy

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/taskevents"
)

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

func framesOfTask(t *testing.T, pub *recordPublisher, taskID string) []taskevents.Event {
	t.Helper()
	var out []taskevents.Event
	for _, ev := range taskFramesOf(t, pub) {
		if ev.TaskID == taskID {
			out = append(out, ev)
		}
	}
	return out
}

func waitForTrackerDrained(t *testing.T, tracker *taskevents.Tracker, projectID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(tracker.Snapshot(projectID)) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("tracker did not drain within 10s")
}

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
		if ev.TaskID != dep.ID.String() || ev.ProjectID != app.ProjectID.String() || ev.Name != app.Name {
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
	if running := tracker.Snapshot(app.ProjectID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after a terminal run", len(running))
	}
}

func TestFailPublishesDespitePersistError(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app, failUpdateState: StateFailed}
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
	if len(frames) == 0 || frames[len(frames)-1].Status != taskevents.StatusFailed {
		t.Fatalf("no terminal failed frame despite persist error: %v", statusesOf(frames))
	}
	if running := tracker.Snapshot(app.ProjectID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after an unpersisted failure", len(running))
	}
}

func TestFailSkipsPublishForAdoptedTerminal(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	seeded := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning})

	pub := &recordPublisher{}
	tracker := taskevents.NewTracker()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Emitter:    NewEmitter(pub),
		Tasks:      tracker,
	})
	st := &runState{
		app:      app,
		dep:      Deployment{ID: seeded.ID, ApplicationID: app.ID, Kind: KindDeploy, State: StateCloning},
		previous: "",
		target:   Target{ServerID: app.ServerID, DeploymentID: seeded.ID},
		rec:      &logRecorder{},
	}
	st.log = func(string) {}

	o.fail(context.Background(), st, ErrAgentUnavailable)

	if st.dep.State != StateRunning {
		t.Errorf("adopted state = %s, want the committed running", st.dep.State)
	}
	for _, ev := range taskFramesOf(t, pub) {
		if ev.Status == taskevents.StatusFailed {
			t.Errorf("published a failed card for an adopted terminal run: %+v", ev)
		}
	}
}

func TestRunPanicStillFailsClosed(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	pub := &recordPublisher{}
	tracker := taskevents.NewTracker()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial: func(context.Context, uuid.UUID) (Node, error) {
			panic("agent exploded")
		},
		Emitter: NewEmitter(pub),
		Tasks:   tracker,
	})

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("run swallowed the panic, want it propagated")
			}
		}()
		o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})
	}()

	stored, ok := repo.deployment(dep.ID)
	if !ok || stored.State != StateFailed {
		t.Fatalf("row = %+v, want a terminal failed row after panic", stored)
	}
	frames := taskFramesOf(t, pub)
	if len(frames) == 0 || frames[len(frames)-1].Status != taskevents.StatusFailed {
		t.Fatalf("no terminal failed frame after panic: %v", statusesOf(frames))
	}
	if running := tracker.Snapshot(app.ProjectID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after a panic", len(running))
	}
}

type ctxCountingPublisher struct {
	recordPublisher
	mu        sync.Mutex
	cancelled int
	live      int
}

func (p *ctxCountingPublisher) Publish(ctx context.Context, channel, payload string) error {
	p.mu.Lock()
	if ctx.Err() != nil {
		p.cancelled++
	} else {
		p.live++
	}
	p.mu.Unlock()
	return p.recordPublisher.Publish(ctx, channel, payload)
}

func (p *ctxCountingPublisher) counts() (live, cancelled int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.live, p.cancelled
}

func TestCancelledRunStillPublishesTerminal(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	pub := &ctxCountingPublisher{}
	tracker := taskevents.NewTracker()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Dial:       func(context.Context, uuid.UUID) (Node, error) { return nil, ErrAgentUnavailable },
		Emitter:    NewEmitter(pub),
		Tasks:      tracker,
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	o.run(cancelled, job{app: app, dep: dep})

	frames := taskFramesOf(t, &pub.recordPublisher)
	if len(frames) == 0 || frames[len(frames)-1].Status != taskevents.StatusFailed {
		t.Fatalf("no terminal failed frame on a cancelled run: %v", statusesOf(frames))
	}
	live, cancelledCount := pub.counts()
	if live == 0 {
		t.Errorf("no task publish ran on a live context (live=%d cancelled=%d)", live, cancelledCount)
	}
	if running := tracker.Snapshot(app.ProjectID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after a cancelled run", len(running))
	}
}

func TestSubmitOrderingStaysMonotonic(t *testing.T) {
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
		Dial:       func(context.Context, uuid.UUID) (Node, error) { return nil, ErrAgentUnavailable },
	})
	t.Cleanup(func() { _ = svc.Close() })

	const runs = 10
	ids := make([]string, 0, runs)
	for i := 0; i < runs; i++ {
		queued, err := svc.DeploySystem(context.Background(), app.ID)
		if err != nil {
			t.Fatalf("deploy %d: %v", i, err)
		}
		ids = append(ids, queued.ID.String())
	}
	waitForTrackerDrained(t, tracker, app.ProjectID.String())

	for _, id := range ids {
		frames := framesOfTask(t, pub, id)
		if len(frames) == 0 {
			t.Fatalf("task %s published nothing", id)
		}
		if frames[0].Status != taskevents.StatusQueued || frames[0].Seq != 1 {
			t.Errorf("task %s first frame = %+v, want queued seq 1", id, frames[0])
		}
		last := frames[len(frames)-1]
		if !last.Status.Terminal() {
			t.Errorf("task %s last frame = %+v, want a terminal status", id, last)
		}
		for i := 1; i < len(frames); i++ {
			if frames[i].Seq != frames[i-1].Seq+1 {
				t.Errorf("task %s frames not sequential: %+v", id, frames)
				break
			}
		}
	}
}

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
		if running := tracker.Snapshot(app.ProjectID.String()); len(running) != 1 {
			t.Errorf("viaSystem=%v: tracker holds %d tasks, want 1", viaSystem, len(running))
		}
	}
}

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
	if running := tracker.Snapshot(app.ProjectID.String()); len(running) != 0 {
		t.Errorf("tracker still holds %d tasks after a failed run", len(running))
	}
}
