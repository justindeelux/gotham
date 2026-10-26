package servers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/justindeelux/gotham/internal/store/sqlc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type stateRecorder struct {
	name    string
	mu      sync.Mutex
	events  []StateChange
	changes chan StateChange
}

func (r *stateRecorder) ServerStateChanged(ctx context.Context, event StateChange) {
	if event.Name != r.name {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	if r.changes != nil {
		r.changes <- event
	}
}

func (r *stateRecorder) snapshot() []StateChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]StateChange(nil), r.events...)
}

func TestServerStateNotifications(t *testing.T) {
	s, st := newTestService(t)
	node := uniqueNodeID("notify")
	recorder := &stateRecorder{name: node, changes: make(chan StateChange, 2)}
	s.SetStateNotifier(recorder)
	ctx := context.Background()
	register := func() {
		t.Helper()
		if _, err := s.RegisterNode(ctx, &agentv1.RegisterRequest{NodeId: node}); err != nil {
			t.Fatal(err)
		}
	}
	register()
	row, err := st.GetServerByNodeID(ctx, &node)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteServer(ctx, row.ID) })
	heartbeat := func() {
		t.Helper()
		if err := s.RecordHeartbeat(ctx, node, &agentv1.HeartbeatRequest{}); err != nil {
			t.Error(err)
		}
	}
	heartbeat()
	if len(recorder.snapshot()) != 0 {
		t.Fatal("first enrollment must not notify")
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	s.sweepAfter = now.Add(heartbeatOfflineAfter)
	if _, err := st.DB.Exec(ctx, "UPDATE servers SET last_seen=$2 WHERE id=$1", row.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	s.sweepOffline(ctx)
	if len(recorder.snapshot()) != 0 {
		t.Fatal("startup grace must suppress offline transitions")
	}
	s.sweepAfter = now.Add(-time.Second)
	if _, err := st.DB.Exec(ctx, "UPDATE servers SET last_seen=$2 WHERE id=$1", row.ID, now.Add(-29*time.Second)); err != nil {
		t.Fatal(err)
	}
	s.sweepOffline(ctx)
	if len(recorder.snapshot()) != 0 {
		t.Fatal("short heartbeat gaps must not notify")
	}
	if _, err := st.DB.Exec(ctx, "UPDATE servers SET last_seen=$2 WHERE id=$1", row.ID, now.Add(-31*time.Second)); err != nil {
		t.Fatal(err)
	}
	loopCtx, cancelLoop := context.WithCancel(ctx)
	t.Cleanup(cancelLoop)
	ticks := make(chan time.Time)
	go s.runOfflineSweep(loopCtx, ticks)
	ticks <- now
	select {
	case <-recorder.changes:
	case <-time.After(5 * time.Second):
		t.Fatal("background sweep did not report an offline server")
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() { s.sweepOffline(ctx) })
	}
	wg.Wait()
	events := recorder.snapshot()
	if len(events) != 1 || events[0].Online || events[0].Name != row.Name {
		t.Fatalf("offline events: %+v", events)
	}
	register()
	if len(recorder.snapshot()) != 1 {
		t.Fatal("registration must preserve offline status")
	}
	for range 6 {
		wg.Go(heartbeat)
	}
	wg.Wait()
	events = recorder.snapshot()
	if len(events) != 2 || !events[1].Online {
		t.Fatalf("recovery events: %+v", events)
	}
	s.sweepOffline(ctx)
	if len(recorder.snapshot()) != 2 {
		t.Fatal("healthy sweep must not repeat notifications")
	}
}

type contextStateNotifier struct {
	callback func(context.Context, StateChange)
}

func (n contextStateNotifier) ServerStateChanged(ctx context.Context, event StateChange) {
	n.callback(ctx, event)
}

func TestStateNotifierDetachesCancellationAndBoundsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	s := &ServerService{now: time.Now, notifier: contextStateNotifier{
		callback: func(ctx context.Context, event StateChange) {
			called = true
			if ctx.Err() != nil {
				t.Fatal("caller cancellation reached notifier")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second {
				t.Fatal("notifier deadline is unbounded")
			}
			if event.Name != "edge" || !event.Online || event.At.IsZero() {
				t.Fatalf("change: %+v", event)
			}
		},
	}}
	s.notifyState(ctx, sqlc.Server{Name: "edge"}, true)
	if !called {
		t.Fatal("notifier was not called")
	}
}
