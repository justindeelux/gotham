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

func TestChannelRoundTrip(t *testing.T) {
	team, ok := TeamOf(Channel("team-1"))
	if !ok || team != "team-1" {
		t.Fatalf("TeamOf(Channel()) = %q, %v", team, ok)
	}
	if _, ok := TeamOf("logs:server:container"); ok {
		t.Fatal("TeamOf accepted a log channel")
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
}
