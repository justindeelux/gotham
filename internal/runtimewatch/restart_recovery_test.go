package runtimewatch

import (
	"testing"
	"time"
)

func TestRestartLoopRecoveryWaitsForLastEvent(t *testing.T) {
	base := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c := container("a")
	c.RestartCount = 1
	state := ReplicaState{ContainerID: "a", RestartCount: 0, ObservedAt: base}
	events, loop, last := updateRestartWindow(state, c, base)
	if len(events) != 1 || loop || !last.Equal(base) {
		t.Fatalf("first restart = %d/%v/%s", len(events), loop, last)
	}

	state = ReplicaState{ContainerID: "a", RestartCount: 1, ObservedAt: base, RestartEvents: events}
	c.RestartCount = 2
	events, _, last = updateRestartWindow(state, c, base.Add(4*time.Minute+50*time.Second))
	if last != base.Add(4*time.Minute+50*time.Second) {
		t.Fatalf("last event = %s", last)
	}

	state = ReplicaState{
		ContainerID:   "a",
		RestartCount:  2,
		ObservedAt:    base.Add(4*time.Minute + 50*time.Second),
		RestartEvents: events,
	}
	c.RestartCount = 3
	lastEvent := base.Add(4*time.Minute + 55*time.Second)
	events, loop, _ = updateRestartWindow(state, c, lastEvent)
	if len(events) != 3 || !loop {
		t.Fatalf("three restarts did not open the loop: %d/%v", len(events), loop)
	}

	state = ReplicaState{ContainerID: "a", RestartCount: 3, ObservedAt: lastEvent, RestartEvents: events}
	events, loop, last = updateRestartWindow(state, c, base.Add(5*time.Minute+time.Second))
	if len(events) != 2 || loop {
		t.Fatalf("window did not age out: %d/%v", len(events), loop)
	}
	if last != lastEvent {
		t.Fatalf("last event = %s, want %s", last, lastEvent)
	}
	if restartRecoveryAllowed(last, base.Add(5*time.Minute+time.Second)) {
		t.Fatal("recovered six seconds after the last restart")
	}
	if !restartRecoveryAllowed(last, lastEvent.Add(restartWindow)) {
		t.Fatal("did not recover five minutes after the last restart")
	}
}
