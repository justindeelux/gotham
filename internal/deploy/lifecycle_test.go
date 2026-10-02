package deploy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// gatedNode wraps a mockNode so a test can hold one RPC open and observe what
// may (or may not) run concurrently with it.
type gatedNode struct {
	*mockNode
	entered sync.Once
	release chan struct{}
	gate    chan struct{}
}

// newGatedNode returns a mock agent whose Start blocks until release is closed,
// signalling on gate once it has been entered.
func newGatedNode() *gatedNode {
	return &gatedNode{
		mockNode: newMockNode(),
		release:  make(chan struct{}),
		gate:     make(chan struct{}),
	}
}

// Start blocks until release is closed, then delegates to the mock.
func (g *gatedNode) Start(ctx context.Context, containerID string) error {
	g.entered.Do(func() { close(g.gate) })
	<-g.release
	return g.mockNode.Start(ctx, containerID)
}

// dialPerServer returns a DialFunc that hands out the node registered for each
// server id and fails like a missing node otherwise.
func dialPerServer(nodes map[uuid.UUID]Node) DialFunc {
	return func(_ context.Context, serverID uuid.UUID) (Node, error) {
		if node, ok := nodes[serverID]; ok {
			return node, nil
		}
		return nil, ErrServerNotFound
	}
}

// TestDeleteApplicationRefusedWhileDeploying is the item-1 regression: a delete
// races the worker between Run and the health/transition and would cascade the
// deployment row, orphaning a running container. The delete must answer
// ErrConflict while any deployment is non-terminal.
func TestDeleteApplicationRefusedWhileDeploying(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	// Build the service first so the boot-time stale sweep does not fail the
	// in-flight row the test is about to seed.
	svc := newNodeService(t, repo, node)
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateStarting, ContainerID: node.containerID})

	err := svc.DeleteApplication(context.Background(), userID, app.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict while a deployment is in flight", err)
	}
	if _, err := svc.GetApplication(context.Background(), userID, app.ID); err != nil {
		t.Errorf("the application must survive a refused delete: %v", err)
	}
	if node.removeCalls != 0 {
		t.Errorf("removed %v, want no container touched while a deployment is in flight", node.removed)
	}
	if got := listStored(t, repo, app.ID); len(got) != 1 {
		t.Errorf("deployments = %d, want the in-flight row untouched", len(got))
	}
}

// TestManualTargetSelectionSerializedWithSubmit is the item-2 regression: the
// active-deployment check must not finish before the agent call while a
// concurrent deploy retires the selected container. With the application lock
// held across target selection and the RPC, a submit cannot cross the queue
// boundary until the manual call completes.
func TestManualTargetSelectionSerializedWithSubmit(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	gated := newGatedNode()
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: gated.containerID})
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Dial:       dialAlways(gated),
	})
	t.Cleanup(func() { _ = svc.Close() })
	repo.createDeploymentNotify = make(chan struct{}, 1)

	started := make(chan struct{})
	go func() {
		defer close(started)
		_, _ = svc.Start(context.Background(), userID, app.ID)
	}()
	select {
	case <-gated.gate:
	case <-time.After(2 * time.Second):
		t.Fatal("manual start never reached the agent call")
	}

	deployDone := make(chan error, 1)
	go func() {
		_, err := svc.Deploy(context.Background(), userID, app.ID)
		deployDone <- err
	}()

	select {
	case <-repo.createDeploymentNotify:
		t.Fatal("submit crossed the queue boundary while a manual start was in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(gated.release)
	<-started
	if err := <-deployDone; err != nil {
		t.Fatalf("deploy after the manual start: %v", err)
	}
}

// TestOrchestratorRetirementFailureFailsClosed is the item-3 regression: a
// previous container that cannot be confirmed stopped must fail the deploy
// closed, never start a second release alongside it.
func TestOrchestratorRetirementFailureFailsClosed(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.stopErr = errors.New("stop refused")
	node.listed = []*agentv1.ContainerInfo{{Id: "old-container-id", State: "running", Status: "Up 1 minute"}}
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed when retirement is unconfirmed", stored.State)
	}
	if node.runCalls != 0 {
		t.Errorf("run calls = %d, want 0 (no replacement while the old release may still run)", node.runCalls)
	}
}

// TestOrchestratorRetirementAbsenceProceeds is the other half of item 3: a stop
// that fails because the container is already gone is a confirmed retirement,
// so the replacement still starts.
func TestOrchestratorRetirementAbsenceProceeds(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.stopErr = errors.New("no such container")
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running when the previous container is verifiably gone", stored.State)
	}
}

// TestOrchestratorRuntimeFailureKeepsPreviousRunning is the item-4 regression:
// the runtime payload is assembled before the previous container is retired, so
// a secret/volume/config failure leaves the live release running.
func TestOrchestratorRuntimeFailureKeepsPreviousRunning(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	repo.secrets = []Secret{{Key: "BROKEN", Ciphertext: "not-a-sealed-value"}}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed on a runtime-payload failure", stored.State)
	}
	if len(node.stopped) != 0 {
		t.Errorf("stopped = %v, want the previous release untouched when the new payload cannot be assembled", node.stopped)
	}
	if node.runCalls != 0 {
		t.Errorf("run calls = %d, want 0", node.runCalls)
	}
}

// TestOrchestratorForcesFailedWhenRunningPersistFails is the item-5 regression:
// a failed `→ running` write must not leave the in-memory state terminal, or
// fail() can no longer record the terminal failure and the row stays wedged in
// a non-terminal state the active-deployment index refuses to release.
func TestOrchestratorForcesFailedWhenRunningPersistFails(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app, failUpdateState: StateRunning}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed (a failed running write must still record a terminal failure)", stored.State)
	}
	if stored.FinishedAt.IsZero() {
		t.Error("finished_at is zero on the forced terminal failure")
	}
}

// TestOrchestratorStartRetryReconcilesOrphan is the item-6 regression: a Run
// whose response was lost can leave a container behind under the deterministic
// name; the retry must reconcile it instead of colliding and orphaning it.
func TestOrchestratorStartRetryReconcilesOrphan(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.runFailures = 1
	node.runErrLeavesContainer = true
	o := newTestOrchestrator(Config{
		Repository:  repo,
		Source:      &fakeSource{},
		Dial:        dialAlways(node),
		MaxAttempts: 2,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateRunning {
		t.Fatalf("state = %s, want running after the retry reconciled the orphan", stored.State)
	}
	if node.runCalls != 2 {
		t.Errorf("run calls = %d, want 2 (one failed, one after reconciliation)", node.runCalls)
	}
	var removedOrphan bool
	for _, id := range node.removed {
		if strings.HasPrefix(id, "orphan-") {
			removedOrphan = true
		}
	}
	if !removedOrphan {
		t.Errorf("removed = %v, want the leftover container removed before the retry", node.removed)
	}
}

// TestOrchestratorRemovesRetiredContainer is the item-8 regression: after the
// replacement runs, the retired container is removed so stopped layers do not
// accumulate.
func TestOrchestratorRemovesRetiredContainer(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep, previous: "old-container-id"})

	if stored, _ := repo.deployment(dep.ID); stored.State != StateRunning {
		t.Fatalf("state = %s, want running", stored.State)
	}
	if !containsString(node.removed, "old-container-id") {
		t.Errorf("removed = %v, want the retired container removed", node.removed)
	}
}

// TestUpdateApplicationStopsContainerOnPreviousNode is the item-7 regression:
// moving an application to another node stops the container it left behind on
// the previous one.
func TestUpdateApplicationStopsContainerOnPreviousNode(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	oldNode := newMockNode()
	newNode := newMockNode()
	newServer := uuid.New()
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: oldNode.containerID})
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Dial:       dialPerServer(map[uuid.UUID]Node{app.ServerID: oldNode, newServer: newNode}),
	})
	t.Cleanup(func() { _ = svc.Close() })

	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{ServerID: &newServer})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ServerID != newServer {
		t.Fatalf("server = %s, want %s", updated.ServerID, newServer)
	}
	if oldNode.stopCalls != 1 || len(oldNode.stopped) != 1 || oldNode.stopped[0] != oldNode.containerID {
		t.Errorf("old node stopped = %v (%d calls), want the container left behind", oldNode.stopped, oldNode.stopCalls)
	}
	if newNode.stopCalls != 0 {
		t.Errorf("new node stop calls = %d, want 0 (nothing runs there yet)", newNode.stopCalls)
	}
}

// TestDeleteApplicationRemovesEveryLabeledContainer is the item-8 delete half:
// every container carrying the application label (including orphans a failed
// deploy left) is removed, while another application's container is untouched.
func TestDeleteApplicationRemovesEveryLabeledContainer(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	node.listed = []*agentv1.ContainerInfo{
		{Id: "orphan-1", Labels: map[string]string{labelAppID: app.ID.String()}},
		{Id: "orphan-2", Labels: map[string]string{labelAppID: app.ID.String()}},
		{Id: "foreign", Labels: map[string]string{labelAppID: uuid.New().String()}},
	}
	svc := newNodeService(t, repo, node)
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: "recorded-container"})

	if err := svc.DeleteApplication(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, want := range []string{"orphan-1", "orphan-2", "recorded-container"} {
		if !containsString(node.removed, want) {
			t.Errorf("removed = %v, want %q removed", node.removed, want)
		}
	}
	if containsString(node.removed, "foreign") {
		t.Errorf("removed = %v, must not touch another application's container", node.removed)
	}
}

// containsString reports membership in a string slice.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
