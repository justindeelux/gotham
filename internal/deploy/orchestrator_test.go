package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
)

// testSecretKey is the key testEnv seals application secrets with; the
// orchestrator must open them with the same value.
const testSecretKey = "test-secret-key"

// newTestOrchestrator builds an orchestrator over the fakes with timers short
// enough for a unit test.
func newTestOrchestrator(cfg Config) *Orchestrator {
	if cfg.Secret == "" {
		cfg.Secret = testSecretKey
	}
	if cfg.HealthPoll == 0 {
		cfg.HealthPoll = 5 * time.Millisecond
	}
	if cfg.HealthTimeout == 0 {
		cfg.HealthTimeout = 2 * time.Second
	}
	if cfg.StepTimeout == 0 {
		cfg.StepTimeout = 5 * time.Second
	}
	if cfg.BuildTimeout == 0 {
		cfg.BuildTimeout = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	return newOrchestrator(cfg)
}

// seedDeployment inserts a deployment row so the orchestrator has something to
// update, mirroring what Service.submit persists before enqueueing.
func seedDeployment(t *testing.T, repo *fakeRepository, app Application, dep Deployment) Deployment {
	t.Helper()
	dep.ApplicationID = app.ID
	if dep.State == "" {
		dep.State = StateQueued
	}
	created, err := repo.CreateDeployment(context.Background(), dep)
	if err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	return created
}

// statesEqual compares two state walks.
func statesEqual(got, want []State) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// renderStates makes a walk readable in failure output.
func renderStates(states []State) string {
	out := make([]string, 0, len(states))
	for _, state := range states {
		out = append(out, string(state))
	}
	return "[" + strings.Join(out, " ") + "]"
}

// dialAlways is a DialFunc that hands out one node.
func dialAlways(node Node) DialFunc {
	return func(context.Context, uuid.UUID) (Node, error) { return node, nil }
}

func TestOrchestratorHappyPath(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t, testSecretKey)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	src := &fakeSource{logs: []string{"checking out " + app.Branch}}
	node := newMockNode()
	pub := &recordPublisher{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     src,
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
	})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	want := []State{StateQueued, StateCloning, StateBuilding, StatePushing, StateStarting, StateRunning}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running", stored.State)
	}
	if stored.Error != "" {
		t.Errorf("error = %q, want empty", stored.Error)
	}
	if stored.StartedAt.IsZero() || stored.FinishedAt.IsZero() {
		t.Errorf("started_at/finished_at not both set: %v / %v", stored.StartedAt, stored.FinishedAt)
	}

	wantTag := builds.ImageTag(app.ID, dep.ID)
	if stored.ImageTag != wantTag {
		t.Errorf("image tag = %q, want %q", stored.ImageTag, wantTag)
	}
	if stored.RegistryImage != "127.0.0.1:5000/"+wantTag {
		t.Errorf("registry image = %q, want %q", stored.RegistryImage, "127.0.0.1:5000/"+wantTag)
	}
	if stored.Digest != node.digest {
		t.Errorf("digest = %q, want %q", stored.Digest, node.digest)
	}
	if stored.ContainerID != node.containerID {
		t.Errorf("container id = %q, want %q", stored.ContainerID, node.containerID)
	}

	if src.calls != 1 {
		t.Errorf("clone calls = %d, want 1", src.calls)
	}
	if src.branch != app.Branch {
		t.Errorf("cloned branch = %q, want %q", src.branch, app.Branch)
	}
	if node.buildCalls != 1 {
		t.Errorf("build calls = %d, want 1", node.buildCalls)
	}
	if node.pullCalls != 1 {
		t.Errorf("pull calls = %d, want 1", node.pullCalls)
	}
	if node.runCalls != 1 {
		t.Errorf("run calls = %d, want 1", node.runCalls)
	}
	if len(node.stopped) != 1 || node.stopped[0] != "old-container-id" {
		t.Errorf("stopped = %v, want [old-container-id]", node.stopped)
	}
	if node.closed != 1 {
		t.Errorf("close calls = %d, want 1 (the agent connection is released)", node.closed)
	}

	req := node.lastRequest()
	if req == nil {
		t.Fatal("no container request recorded")
	}
	if req.Image != stored.RegistryImage {
		t.Errorf("image = %q, want %q", req.Image, stored.RegistryImage)
	}
	if !equalStrings(req.Env, []string{"API_TOKEN=hunter2", "FOO=bar", "PORT=3000"}) {
		t.Errorf("env = %v, want decrypted secret, plain variable and the PORT default", req.Env)
	}
	if !equalStrings(req.Ports, []string{"8080:3000"}) {
		t.Errorf("ports = %v, want [8080:3000]", req.Ports)
	}
	if req.Labels[labelManaged] != "true" {
		t.Errorf("label %s = %q, want true", labelManaged, req.Labels[labelManaged])
	}
	if req.Labels[labelDeploymentID] != dep.ID.String() {
		t.Errorf("label %s = %q, want %q", labelDeploymentID, req.Labels[labelDeploymentID], dep.ID.String())
	}
	if !strings.HasPrefix(req.Name, "gotham-demo-app-") {
		t.Errorf("container name = %q, want a gotham-demo-app-* name", req.Name)
	}

	wantChannel := DeployChannel(app.ServerID, dep.ID)
	var sawTransition, sawImage, sawStop bool
	for _, event := range pub.payloads() {
		if event.Channel != wantChannel {
			t.Errorf("channel = %q, want %q", event.Channel, wantChannel)
		}
		if event.Type != "log" {
			t.Errorf("event type = %q, want log", event.Type)
		}
		sawTransition = sawTransition || strings.Contains(event.Data, "queued → cloning")
		sawImage = sawImage || strings.Contains(event.Data, "image built: ")
		sawStop = sawStop || strings.Contains(event.Data, "stopped previous container")
	}
	if !sawTransition {
		t.Error("no state transition event published")
	}
	if !sawImage {
		t.Error("no image-built event published")
	}
	if !sawStop {
		t.Error("no previous-container event published")
	}
}

func TestOrchestratorStreamsBuildLogs(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.buildLogs = []string{"Step 1/2 FROM node:20\n", "Step 2/2 COPY . .\n"}
	pub := &recordPublisher{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	seen := map[string]bool{}
	for _, event := range pub.payloads() {
		seen[event.Data] = true
	}
	for _, line := range []string{"Step 1/2 FROM node:20", "Step 2/2 COPY . ."} {
		if !seen[line] {
			t.Errorf("build log line %q was not published as its own event", line)
		}
	}
	if stored, _ := repo.deployment(dep.ID); stored.State != StateRunning {
		t.Errorf("state = %s, want running", stored.State)
	}
}

func TestOrchestratorBuildFailure(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.buildErr = errors.New("compile failed")
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	want := []State{StateQueued, StateCloning, StateBuilding, StateFailed}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed", stored.State)
	}
	if !strings.Contains(stored.Error, "compile failed") {
		t.Errorf("error = %q, want it to quote the build failure", stored.Error)
	}
	if stored.FinishedAt.IsZero() {
		t.Error("finished_at not set on a terminal failure")
	}
	if stored.ImageTag != "" {
		t.Errorf("image tag = %q, want empty (the build never produced one)", stored.ImageTag)
	}
	if node.runCalls != 0 {
		t.Errorf("run calls = %d, want 0 (a failed build never starts a container)", node.runCalls)
	}
	if node.closed != 1 {
		t.Errorf("close calls = %d, want 1", node.closed)
	}
}

func TestOrchestratorHealthcheckFailure(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.healthState = "running"
	node.healthStatus = "(unhealthy)"
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	want := []State{StateQueued, StateCloning, StateBuilding, StatePushing, StateStarting, StateFailed}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
	if !strings.Contains(stored.Error, ErrHealthcheck.Error()) {
		t.Errorf("error = %q, want a healthcheck failure", stored.Error)
	}
	if node.runCalls != 1 {
		t.Errorf("run calls = %d, want 1", node.runCalls)
	}
	if stored.ContainerID == "" {
		t.Error("container id not persisted before the healthcheck failed")
	}
}

func TestOrchestratorHealthcheckTimeout(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.healthState = "running"
	node.healthStatus = "(health: starting)"
	o := newTestOrchestrator(Config{
		Repository:    repo,
		Source:        &fakeSource{},
		Dial:          dialAlways(node),
		HealthTimeout: 50 * time.Millisecond,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed", stored.State)
	}
	if !strings.Contains(stored.Error, "did not become healthy within") {
		t.Errorf("error = %q, want the health window message", stored.Error)
	}
}

func TestOrchestratorRetriesAgentUnavailable(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	dial, calls := dialScript(node, 2, nil)
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dial,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	if *calls != 3 {
		t.Errorf("dial calls = %d, want 3 (two failures then success)", *calls)
	}
	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running after the retries", stored.State)
	}
	want := []State{StateQueued, StateCloning, StateBuilding, StatePushing, StateStarting, StateRunning}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
}

func TestOrchestratorExhaustsAgentRetries(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	dial, calls := dialScript(node, 100, nil)
	o := newTestOrchestrator(Config{
		Repository:  repo,
		Source:      &fakeSource{},
		Dial:        dial,
		MaxAttempts: 3,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	if *calls != 3 {
		t.Errorf("dial calls = %d, want 3 (the configured retry budget)", *calls)
	}
	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed", stored.State)
	}
	if stored.Attempt != 3 {
		t.Errorf("attempt = %d, want 3 (the last attempt counter is persisted)", stored.Attempt)
	}
	if !strings.Contains(stored.Error, ErrAgentUnavailable.Error()) {
		t.Errorf("error = %q, want it to name the unavailable agent", stored.Error)
	}
	if node.closed != 0 {
		t.Errorf("close calls = %d, want 0 (no connection was ever opened)", node.closed)
	}
}

func TestOrchestratorCloneFailureIsTerminal(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	src := &fakeSource{err: errors.New("repository not found")}
	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository:  repo,
		Source:      src,
		Dial:        dialAlways(node),
		MaxAttempts: 3,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	if src.calls != 1 {
		t.Errorf("clone calls = %d, want 1 (only agent outages are retried)", src.calls)
	}
	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed", stored.State)
	}
	if !strings.Contains(stored.Error, "repository not found") {
		t.Errorf("error = %q, want the clone failure", stored.Error)
	}
	if node.buildCalls != 0 {
		t.Errorf("build calls = %d, want 0", node.buildCalls)
	}
}

func TestOrchestratorRollbackRedeploysPreviousImage(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	src := &fakeSource{}
	node := newMockNode()
	dep := seedDeployment(t, repo, app, Deployment{
		Kind:          KindRollback,
		ImageTag:      "gotham/previous:release",
		RegistryImage: "127.0.0.1:5000/gotham/previous:release",
		Digest:        "sha256:0123456789abcdef",
		RollbackFrom:  uuid.New(),
	})
	pub := &recordPublisher{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     src,
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
	})

	o.run(context.Background(), job{app: app, dep: dep, previous: "release-container"})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	want := []State{StateQueued, StatePushing, StateStarting, StateRunning}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running", stored.State)
	}
	if stored.ImageTag != "gotham/previous:release" {
		t.Errorf("image tag = %q, want the rolled-back tag", stored.ImageTag)
	}
	if src.calls != 0 {
		t.Errorf("clone calls = %d, want 0 (a rollback reuses a built image)", src.calls)
	}
	if node.buildCalls != 0 {
		t.Errorf("build calls = %d, want 0 (a rollback reuses a built image)", node.buildCalls)
	}
	if node.pullCalls != 1 {
		t.Errorf("pull calls = %d, want 1", node.pullCalls)
	}
	if len(node.stopped) != 1 || node.stopped[0] != "release-container" {
		t.Errorf("stopped = %v, want [release-container]", node.stopped)
	}
	req := node.lastRequest()
	if req == nil {
		t.Fatal("no container request recorded")
	}
	if req.Image != "127.0.0.1:5000/gotham/previous:release" {
		t.Errorf("image = %q, want the previous registry reference", req.Image)
	}
}

func TestPushStep(t *testing.T) {
	t.Run("node build confirms the registry reference", func(t *testing.T) {
		node := newMockNode()
		st := &runState{
			node: node,
			dep:  Deployment{ImageTag: "gotham/app:1", RegistryImage: "127.0.0.1:5000/gotham/app:1"},
			log:  func(string) {},
		}
		if err := newTestOrchestrator(Config{}).push(context.Background(), st); err != nil {
			t.Fatalf("push: %v", err)
		}
		if node.pullCalls != 1 {
			t.Errorf("pull calls = %d, want 1", node.pullCalls)
		}
	})

	t.Run("control plane build logs the missing transport", func(t *testing.T) {
		node := newMockNode()
		var lines []string
		st := &runState{
			node: node,
			dep:  Deployment{ImageTag: "gotham/app:2"},
			log:  func(line string) { lines = append(lines, line) },
		}
		if err := newTestOrchestrator(Config{}).push(context.Background(), st); err != nil {
			t.Fatalf("push: %v", err)
		}
		if node.pullCalls != 0 {
			t.Errorf("pull calls = %d, want 0 (no registry reference exists)", node.pullCalls)
		}
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "built on the control plane") {
			t.Errorf("log = %q, want it to explain the control-plane build", joined)
		}
	})
}
