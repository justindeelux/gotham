package runtimewatch

import "testing"

func TestSummarizeTreatsOldOOMAsEvidenceOnly(t *testing.T) {
	recreated := replicaRow{State: "running", Health: "healthy", ContainerID: "b", OOMContainer: "a"}
	restarted := replicaRow{State: "running", Health: "healthy", ContainerID: "a", OOMContainer: "a"}
	stillDown := replicaRow{State: "stopped", Health: "unknown", Reason: "stopped", ContainerID: "a", OOMContainer: "a"}
	starting := replicaRow{State: "starting", Health: "starting", ContainerID: "a", OOMContainer: "a"}

	if got, _ := aggregateRows([]replicaRow{recreated}); got != StateHealthy {
		t.Fatalf("recreated container = %q, want healthy", got)
	}
	if got, _ := aggregateRows([]replicaRow{restarted}); got != StateHealthy {
		t.Fatalf("policy-restarted container = %q, want healthy", got)
	}
	if got, reason := aggregateRows([]replicaRow{stillDown}); got != StateUnavailable || reason != "oom_killed" {
		t.Fatalf("still-terminated OOM = %q/%q, want unavailable/oom_killed", got, reason)
	}
	if got, _ := aggregateRows([]replicaRow{starting}); got != StateStarting {
		t.Fatalf("restarting OOM container = %q, want starting", got)
	}
}
