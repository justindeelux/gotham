package taskevents

import (
	"context"
	"encoding/json"
	"testing"
)

type memPub struct {
	msgs map[string][]string
}

func (m *memPub) Publish(_ context.Context, channel, payload string) error {
	if m.msgs == nil {
		m.msgs = make(map[string][]string)
	}
	m.msgs[channel] = append(m.msgs[channel], payload)
	return nil
}

func TestChannelShape(t *testing.T) {
	if Channel("team-1") != "tasks:team-1" {
		t.Fatalf("Channel() = %q", Channel("team-1"))
	}
}

func TestEmitPublishesFramedEvent(t *testing.T) {
	pub := &memPub{}
	em := NewEmitter(pub)
	em.Emit(context.Background(), Event{TaskID: "d1", Kind: KindDeploy, Name: "web", Status: StatusRunning, Step: "building", Progress: 50, TeamID: "t1"})
	got := pub.msgs[Channel("t1")]
	if len(got) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(got))
	}
	var frame Frame
	if err := json.Unmarshal([]byte(got[0]), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != frameType || frame.Channel != Channel("t1") {
		t.Fatalf("unexpected frame %+v", frame)
	}
	var ev Event
	if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Step != "building" || ev.Progress != 50 {
		t.Fatalf("unexpected event %+v", ev)
	}
}

func TestNilEmitterIsNoOp(t *testing.T) {
	var em *Emitter
	em.Emit(context.Background(), Event{TaskID: "x"})
	NewEmitter(nil).Emit(context.Background(), Event{TaskID: "x"})
}

func TestTrackerSnapshotPerTeam(t *testing.T) {
	tr := NewTracker()
	tr.Set(Event{TaskID: "a", TeamID: "t1", Status: StatusRunning})
	tr.Set(Event{TaskID: "b", TeamID: "t2", Status: StatusQueued})
	tr.Set(Event{TaskID: "a", TeamID: "t1", Status: StatusSucceeded})
	if got := tr.Snapshot("t1"); len(got) != 0 {
		t.Fatalf("terminal event should leave the tracker, got %v", got)
	}
	if got := tr.Snapshot("t2"); len(got) != 1 || got[0].TaskID != "b" {
		t.Fatalf("unexpected snapshot %v", got)
	}
	frames := tr.SnapshotFrames("t2")
	if len(frames) != 2 {
		t.Fatalf("expected 1 event + end marker, got %d", len(frames))
	}
	var end Frame
	if err := json.Unmarshal([]byte(frames[1]), &end); err != nil || end.Type != SnapshotType {
		t.Fatalf("missing snapshot end marker: %v %v", frames[1], err)
	}
	if len(tr.SnapshotFrames("nobody")) != 1 {
		t.Fatal("empty snapshot should still close with the end marker")
	}
	var nilTracker *Tracker
	if nilTracker.Snapshot("t") != nil || len(nilTracker.SnapshotFrames("t")) != 1 {
		t.Fatal("nil tracker should behave as empty")
	}
	if nilTracker.Next("x") != 0 {
		t.Fatal("nil tracker Next should be zero")
	}
}

func TestNextIsMonotonicPerTask(t *testing.T) {
	tr := NewTracker()
	if got := []uint64{tr.Next("a"), tr.Next("a"), tr.Next("b"), tr.Next("a")}; got[0] != 1 || got[1] != 2 || got[2] != 1 || got[3] != 3 {
		t.Fatalf("Next() = %v, want [1 2 1 3]", got)
	}
}

func TestSnapshotIsSortedByTaskID(t *testing.T) {
	tr := NewTracker()
	for _, id := range []string{"c", "a", "b"} {
		tr.Set(Event{TaskID: id, TeamID: "t1", Status: StatusRunning})
	}
	got := tr.Snapshot("t1")
	if len(got) != 3 || got[0].TaskID != "a" || got[1].TaskID != "b" || got[2].TaskID != "c" {
		t.Fatalf("unordered snapshot %v", got)
	}
}
