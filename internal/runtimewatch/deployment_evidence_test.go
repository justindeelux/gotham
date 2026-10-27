package runtimewatch

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateReplicaKeepsDeploymentEvidence(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c := container("a")
	c.DeploymentID = "dep-123"
	ev := replicaEvidence{Container: c, Group: []ContainerEvidence{c}}
	state := evaluateReplica(ev, ReplicaState{}, now, now, "")
	if state.DeploymentID != "dep-123" {
		t.Fatalf("deployment evidence = %q", state.DeploymentID)
	}
	if !strings.Contains(replicaEvidenceText(state), "deployment=dep-123") {
		t.Fatalf("incident evidence omits deployment id: %s", replicaEvidenceText(state))
	}
}
