package runtimewatch

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/services"
)

func at(offset time.Duration) *time.Time {
	value := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC).Add(offset)
	return &value
}

func container(id string) ContainerEvidence {
	return ContainerEvidence{ID: id, State: "running", Health: "healthy", RestartPolicy: "always", ExitCode: 0}
}

func TestAssociationUsesOwnershipRules(t *testing.T) {
	app := uuid.New()
	compose := Target{ResourceType: ResourceApplication, ResourceID: app, Compose: true}
	web1 := container("a1")
	web1.ComposeProject = services.ProjectName(app)
	web1.ComposeService = "web"
	web1.Replica = "1"
	web2 := container("a2")
	web2.ComposeProject = services.ProjectName(app)
	web2.ComposeService = "web"
	web2.Replica = "2"
	foreign := container("a3")
	foreign.ComposeProject = services.ProjectName(uuid.New())
	foreign.ComposeService = "web"
	foreign.Replica = "1"
	registry := container("a4")
	registry.ComposeProject = services.ProjectName(app)
	registry.ComposeService = "web"
	registry.Replica = "1"
	registry.Role = "registry"

	replicas := associatedReplicas(compose, []ContainerEvidence{web1, web2, foreign, registry})
	if len(replicas) != 2 {
		t.Fatalf("replicas=%d, want 2: %+v", len(replicas), replicas)
	}
	if replicas[0].Replica != "1" || replicas[1].Replica != "2" {
		t.Fatalf("replicas not distinct: %+v", replicas)
	}

	plain := Target{ResourceType: ResourceApplication, ResourceID: app}
	owned := container("b1")
	owned.AppID = app.String()
	other := container("b2")
	other.AppID = uuid.New().String()
	plainReplicas := associatedReplicas(plain, []ContainerEvidence{owned, other})
	if len(plainReplicas) != 1 || plainReplicas[0].Container.ID != "b1" {
		t.Fatalf("plain app ownership: %+v", plainReplicas)
	}

	dbID := uuid.New()
	database := Target{ResourceType: ResourceDatabase, ResourceID: dbID}
	ownedDB := container("c1")
	ownedDB.DatabaseID = dbID.String()
	otherDB := container("c2")
	otherDB.DatabaseID = uuid.New().String()
	dbReplicas := associatedReplicas(database, []ContainerEvidence{ownedDB, otherDB})
	if len(dbReplicas) != 1 || dbReplicas[0].Container.ID != "c1" {
		t.Fatalf("database ownership: %+v", dbReplicas)
	}
}

func TestPreferRunningReplica(t *testing.T) {
	app := uuid.New()
	old := container("old")
	old.AppID = app.String()
	old.State = "exited"
	old.StartedAt = at(-time.Hour)
	newOne := container("new")
	newOne.AppID = app.String()
	newOne.StartedAt = at(-time.Minute)
	replicas := associatedReplicas(Target{ResourceType: ResourceApplication, ResourceID: app}, []ContainerEvidence{old, newOne})
	if len(replicas) != 1 || replicas[0].Container.ID != "new" {
		t.Fatalf("running replica not preferred: %+v", replicas)
	}
}

func TestOneShotCompletion(t *testing.T) {
	c := container("job")
	c.State = "exited"
	c.RestartPolicy = "no"
	c.ExitCode = 0
	if !oneShotCompleted(c) {
		t.Fatal("successful job not recognized")
	}
	c.ExitCode = 137
	if oneShotCompleted(c) {
		t.Fatal("non-zero exit treated as completed job")
	}
	c.ExitCode = 0
	c.RestartPolicy = "always"
	if oneShotCompleted(c) {
		t.Fatal("restarting service treated as one-shot")
	}
}

func TestRestartWindowBaselineResetAndLoop(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c := container("a")
	c.RestartCount = 4
	state := ReplicaState{ContainerID: "a", RestartCount: 4, ObservedAt: now.Add(-15 * time.Second)}

	events, loop := updateRestartWindow(state, c, now)
	if len(events) != 0 || loop {
		t.Fatalf("first sample produced events=%d loop=%v", len(events), loop)
	}

	c.RestartCount = 7
	events, loop = updateRestartWindow(state, c, now)
	if len(events) != 3 || !loop {
		t.Fatalf("three increases did not trigger: events=%d loop=%v", len(events), loop)
	}

	c.RestartCount = 1 // counter reset after recreate
	events, loop = updateRestartWindow(state, c, now)
	if len(events) != 0 || loop {
		t.Fatalf("counter reset produced events=%d loop=%v", len(events), loop)
	}

	state = ReplicaState{ContainerID: "a", RestartCount: 10, ObservedAt: now.Add(-15 * time.Second)}
	c = container("b")
	c.RestartCount = 13
	if events, loop = updateRestartWindow(state, c, now); len(events) != 0 || loop {
		t.Fatalf("recreated container inherited a previous window: %d %v", len(events), loop)
	}

	state = ReplicaState{
		ContainerID:   "a",
		RestartCount:  10,
		ObservedAt:    now.Add(-15 * time.Second),
		RestartEvents: []time.Time{now.Add(-6 * time.Minute)},
	}
	c = container("a")
	c.RestartCount = 13
	events, loop = updateRestartWindow(state, c, now)
	if len(events) != 3 || !loop {
		t.Fatalf("old events were not pruned: %d %v", len(events), loop)
	}
}

func TestRestartWindowStaleGapBaselines(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c := container("a")
	c.RestartCount = 9
	state := ReplicaState{ContainerID: "a", RestartCount: 0, ObservedAt: now.Add(-10 * time.Minute)}
	events, loop := updateRestartWindow(state, c, now)
	if len(events) != 0 || loop {
		t.Fatalf("stale gap produced events=%d loop=%v", len(events), loop)
	}
	// The next sample still needs two further increases to reach the threshold.
	state.RestartCount = 9
	state.ObservedAt = now
	c.RestartCount = 11
	events, loop = updateRestartWindow(state, c, now.Add(15*time.Second))
	if len(events) != 2 || loop {
		t.Fatalf("events after baseline=%d loop=%v", len(events), loop)
	}
}

func TestSuppressionReason(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	grace := DefaultSettings()
	grace.Grace = 120 * time.Second

	stopped := Target{ResourceType: ResourceApplication, Settings: Settings{DesiredState: "stopped"}}
	if got := suppressionReason(stopped, now); got != "intentionally_stopped" {
		t.Fatalf("stopped app=%q", got)
	}
	database := Target{ResourceType: ResourceDatabase, DatabaseStatus: "stopped", Settings: DefaultSettings()}
	if got := suppressionReason(database, now); got != "intentionally_stopped" {
		t.Fatalf("stopped database=%q", got)
	}
	deploying := Target{
		ResourceType:      ResourceApplication,
		Settings:          DefaultSettings(),
		DeploymentState:   "building",
		DeploymentStarted: at(-time.Minute),
	}
	if got := suppressionReason(deploying, now); got != "deployment_in_progress" {
		t.Fatalf("deploying=%q", got)
	}
	deploying.DeploymentStarted = at(-maxOperationSuppression - time.Minute)
	if got := suppressionReason(deploying, now); got != "" {
		t.Fatalf("stuck deployment still suppressed: %q", got)
	}
	justDeployed := Target{
		ResourceType:       ResourceApplication,
		Settings:           grace,
		DeploymentState:    "running",
		DeploymentFinished: at(-30 * time.Second),
	}
	if got := suppressionReason(justDeployed, now); got != "startup_grace" {
		t.Fatalf("startup grace=%q", got)
	}
	grace.GraceUntil = now.Add(time.Minute)
	started := Target{ResourceType: ResourceApplication, Settings: grace}
	if got := suppressionReason(started, now); got != "startup_grace" {
		t.Fatalf("start grace=%q", got)
	}
}

func TestEvaluateReplicaStates(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		container  ContainerEvidence
		wantState  string
		wantReason string
	}{
		{"healthy", ContainerEvidence{ID: "a", State: "running", Health: "healthy"}, "running", ""},
		{"no healthcheck", ContainerEvidence{ID: "a", State: "running"}, "running", "no_healthcheck"},
		{"unhealthy", ContainerEvidence{ID: "a", State: "running", Health: "unhealthy"}, "unhealthy", "unhealthy"},
		{"starting", ContainerEvidence{ID: "a", State: "running", Health: "starting"}, "starting", "starting"},
		{"stopped", ContainerEvidence{ID: "a", State: "exited"}, "stopped", "stopped"},
		{"restarting", ContainerEvidence{ID: "a", State: "restarting"}, "unhealthy", "restarting"},
		{"one-shot", ContainerEvidence{ID: "a", State: "exited", RestartPolicy: "no", ExitCode: 0}, "stopped", "one_shot_completed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := replicaEvidence{Service: "web", Replica: "1", Container: tc.container, Group: []ContainerEvidence{tc.container}}
			got := evaluateReplica(ev, ReplicaState{}, now, now, "")
			if got.State != tc.wantState || got.Reason != tc.wantReason {
				t.Fatalf("state=%q reason=%q, want %q %q", got.State, got.Reason, tc.wantState, tc.wantReason)
			}
		})
	}
}

func TestEvaluateReplicaSuppression(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ev := replicaEvidence{Service: "web", Replica: "1", Container: ContainerEvidence{ID: "a", State: "exited"}}
	stopped := evaluateReplica(ev, ReplicaState{}, now, now, "intentionally_stopped")
	if stopped.State != "stopped" || stopped.Reason != "intentionally_stopped" {
		t.Fatalf("intentional stop=%+v", stopped)
	}
	deploying := evaluateReplica(ev, ReplicaState{}, now, now, "deployment_in_progress")
	if deploying.State != "starting" {
		t.Fatalf("deployment suppression=%+v", deploying)
	}
}

func TestOOMOccurrenceDeduplication(t *testing.T) {
	finished := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c := ContainerEvidence{ID: "a", State: "exited", OOMKilled: true, FinishedAt: &finished}
	state := ReplicaState{}
	first := oomOccurrence(state, []ContainerEvidence{c})
	if first == nil || first.ID != "a" {
		t.Fatalf("first OOM not reported: %+v", first)
	}
	state.OOMContainer = "a"
	state.OOMAt = finished.Add(time.Second)
	if again := oomOccurrence(state, []ContainerEvidence{c}); again != nil {
		t.Fatalf("old OOM reported twice: %+v", again)
	}
	later := finished.Add(time.Minute)
	c.FinishedAt = &later
	if next := oomOccurrence(state, []ContainerEvidence{c}); next == nil {
		t.Fatal("new OOM after a later termination was missed")
	}
	recreated := ContainerEvidence{ID: "b", State: "exited", OOMKilled: true, FinishedAt: &later}
	if next := oomOccurrence(state, []ContainerEvidence{recreated}); next == nil || next.ID != "b" {
		t.Fatalf("new container OOM missed: %+v", next)
	}
	if guessed := oomOccurrence(state, []ContainerEvidence{{ID: "c", State: "exited", ExitCode: 137}}); guessed != nil {
		t.Fatal("exit code 137 alone was treated as OOM")
	}
}

func TestSummarizeStates(t *testing.T) {
	row := func(state, health, reason string) replicaRow {
		return replicaRow{State: state, Health: health, Reason: reason}
	}
	cases := []struct {
		name string
		rows []replicaRow
		want string
	}{
		{"healthy", []replicaRow{row("running", "healthy", "")}, StateHealthy},
		{"degraded", []replicaRow{row("running", "healthy", ""), row("unhealthy", "unhealthy", "unhealthy")}, StateDegraded},
		{"unavailable", []replicaRow{row("missing", "unknown", "missing")}, StateUnavailable},
		{"starting", []replicaRow{row("starting", "starting", "starting")}, StateStarting},
		{"unknown", []replicaRow{row("running", "unknown", "no_healthcheck")}, StateUnknown},
		{"stopped", []replicaRow{row("stopped", "unknown", "one_shot_completed")}, StateStopped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := aggregateRows(tc.rows)
			if got != tc.want {
				t.Fatalf("state=%q, want %q", got, tc.want)
			}
		})
	}
}
