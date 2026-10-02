package deploy

import (
	"context"
	"errors"
	"fmt"
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

// blockingNode is a mock agent whose Stop blocks until its context ends,
// returning ErrAgentUnavailable, so a test can exercise the manual-control
// timeout.
type blockingNode struct {
	*mockNode
	stopStarted chan struct{}
}

// newBlockingNode returns a mock agent whose Stop never answers on its own.
func newBlockingNode() *blockingNode {
	return &blockingNode{mockNode: newMockNode(), stopStarted: make(chan struct{}, 1)}
}

// Stop blocks until ctx is done, then reports the agent unreachable.
func (b *blockingNode) Stop(ctx context.Context, _ string) error {
	select {
	case b.stopStarted <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return fmt.Errorf("%w: %v", ErrAgentUnavailable, ctx.Err())
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

// TestSubmitUsesFreshApplicationAfterMove is the review regression for the
// stale snapshot: a caller that loaded the application before a move must not
// have the worker deploy to the previous node. submit re-reads the stored row
// under the application lock.
func TestSubmitUsesFreshApplicationAfterMove(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID) // snapshot on the old server
	repo := &fakeRepository{app: app}
	oldNode := newMockNode()
	newNode := newMockNode()
	newServer := uuid.New()

	// The move lands after the caller took its snapshot.
	moved := app
	moved.ServerID = newServer
	if _, err := repo.UpdateApplication(context.Background(), moved); err != nil {
		t.Fatalf("move: %v", err)
	}

	var mu sync.Mutex
	var dialed []uuid.UUID
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Emitter:    NewEmitter(&recordPublisher{}),
		Dial: func(_ context.Context, serverID uuid.UUID) (Node, error) {
			mu.Lock()
			dialed = append(dialed, serverID)
			mu.Unlock()
			switch serverID {
			case app.ServerID:
				return oldNode, nil
			case newServer:
				return newNode, nil
			default:
				return nil, ErrServerNotFound
			}
		},
	})
	t.Cleanup(func() { _ = svc.Close() })

	// stale is the caller's pre-move snapshot; submit must ignore it in favour
	// of the stored row.
	stale := app
	created, err := svc.submit(context.Background(), stale, Deployment{Kind: KindDeploy, State: StateQueued})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var stored Deployment
	for {
		var ok bool
		stored, ok = repo.deployment(created.ID)
		if ok && stored.State.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment did not reach a terminal state: %+v", stored)
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	got := append([]uuid.UUID(nil), dialed...)
	mu.Unlock()
	for _, id := range got {
		if id == app.ServerID {
			t.Errorf("worker dialed the previous server %s; submit used a stale snapshot", app.ServerID)
		}
	}
	var sawNew bool
	for _, id := range got {
		sawNew = sawNew || id == newServer
	}
	if !sawNew {
		t.Errorf("dialed = %v, want the fresh server %s", got, newServer)
	}
}

// TestUpdateApplicationRefusesMoveWhileDeploying is the review regression for
// the move path: changing the server while a deployment is non-terminal would
// leave an untracked container on the previous node, so it is refused like a
// delete or a manual control.
func TestUpdateApplicationRefusesMoveWhileDeploying(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	// Build the service first so the stale sweep does not fail the row.
	svc := newNodeService(t, repo, node)
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateStarting, ContainerID: node.containerID})
	newServer := uuid.New()

	if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
		UpdateApplicationInput{ServerID: &newServer}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict while a deployment is in flight", err)
	}
	got, err := svc.GetApplication(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("get application: %v", err)
	}
	if got.ServerID != app.ServerID {
		t.Errorf("server = %s, want the move refused and the old server %s kept", got.ServerID, app.ServerID)
	}
}

// TestManualControlReadsServerUnderLock pins the control-container re-read: a
// concurrent move that lands after the first read but before the lock is held
// must still make the manual call target the node the application is bound to
// once the lock is held.
func TestManualControlReadsServerUnderLock(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	oldNode := newMockNode()
	newNode := newMockNode()
	newServer := uuid.New()
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: oldNode.containerID})

	// The first GetApplication (before the lock) returns the old server; the
	// hook moves the stored row immediately afterwards, so only the re-read
	// under the lock sees the new server.
	var once sync.Once
	repo.onGetApplication = func() {
		once.Do(func() {
			moved := app
			moved.ServerID = newServer
			repo.mu.Lock()
			repo.app = moved
			repo.mu.Unlock()
		})
	}

	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Dial:       dialPerServer(map[uuid.UUID]Node{app.ServerID: oldNode, newServer: newNode}),
	})
	t.Cleanup(func() { _ = svc.Close() })

	if _, err := svc.Start(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if newNode.startCalls != 1 {
		t.Errorf("new node start calls = %d, want the manual start to use the server read under the lock", newNode.startCalls)
	}
	if oldNode.startCalls != 0 {
		t.Errorf("old node start calls = %d, want 0 (a stale pre-lock snapshot must not be used)", oldNode.startCalls)
	}
}

// TestManualControlBoundedByTimeout is the T2 regression: a hung agent must not
// pin the application lock; the manual call is bounded by ControlTimeout.
func TestManualControlBoundedByTimeout(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newBlockingNode()
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: node.containerID})
	svc := NewService(Config{
		Repository:     repo,
		Secret:         testSecretKey,
		Logger:         discardLogger(),
		Dial:           dialAlways(node),
		ControlTimeout: 30 * time.Millisecond,
	})
	t.Cleanup(func() { _ = svc.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err := svc.Stop(ctx, userID, app.ID)
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("err = %v, want ErrAgentUnavailable", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Errorf("stop took %s, want the 30ms control timeout to bound it", elapsed)
	}
}

// TestOrchestratorFailFromQueuedSetsStartedAt is the review regression for
// forceFail: a failure before the run ever leaves queued (for example a dial
// failure) still records the deployment start clock.
func TestOrchestratorFailFromQueuedSetsStartedAt(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	dial, _ := dialScript(node, 100, nil)
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dial, MaxAttempts: 1})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if stored.StartedAt.IsZero() {
		t.Error("started_at is zero on a failure from queued")
	}
}

// TestOrchestratorReconcilesOrphanFromDifferentDeployment is the T1 regression:
// an orphan a lost Run response left behind (its deployment id never recorded)
// survives while the agent is down; a later deploy must clear it by app label
// before starting, or a pinned host port wedges every later deploy.
func TestOrchestratorReconcilesOrphanFromDifferentDeployment(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	first := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.runFailures = 1
	node.runErrLeavesContainer = true
	node.removeErr = errors.New("agent down")
	o := newTestOrchestrator(Config{
		Repository:  repo,
		Source:      &fakeSource{},
		Dial:        dialAlways(node),
		MaxAttempts: 1,
	})

	o.run(context.Background(), job{app: app, dep: first})
	if stored, _ := repo.deployment(first.ID); stored.State != StateFailed {
		t.Fatalf("first state = %s, want failed", stored.State)
	}
	if len(node.orphans) != 1 {
		t.Fatalf("orphans = %d, want the one left by the lost response", len(node.orphans))
	}

	// The agent recovers. The next deployment must remove the orphan — whose
	// deployment id differs — before it starts.
	node.removeErr = nil
	node.runFailures = 0
	node.runErrLeavesContainer = false
	second := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	o.run(context.Background(), job{app: app, dep: second})

	if stored, _ := repo.deployment(second.ID); stored.State != StateRunning {
		t.Errorf("second state = %s, want running after the orphan was cleared", stored.State)
	}
	if len(node.orphans) != 0 {
		t.Errorf("orphans = %d, want 0 (the app-label reconcile clears the orphan)", len(node.orphans))
	}
	if !containsString(node.removed, "orphan-"+containerName(app, first)) {
		t.Errorf("removed = %v, want the orphan removed by app label", node.removed)
	}
}

// TestTransitionKeepsStateOnPersistFailure pins the transition copy-on-write:
// a failed persist must not advance the in-memory state past what the database
// stored.
func TestTransitionKeepsStateOnPersistFailure(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app, failUpdateState: StateCloning}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(newMockNode())})
	st := &runState{app: app, dep: dep, log: func(string) {}}

	if err := o.transition(context.Background(), st, StateCloning); err == nil {
		t.Fatal("transition succeeded although the persist failed")
	}
	if st.dep.State != StateQueued {
		t.Errorf("in-memory state = %s, want queued after a failed persist", st.dep.State)
	}
}

// TestForceFailKeepsCommittedRunning pins the forceFail guard: an ambiguous
// write that committed the running state but reported an error must not be
// downgraded to failed by the failure path.
func TestForceFailKeepsCommittedRunning(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app, updateErrAfterPersist: StateRunning}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	node := newMockNode()
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, _ := repo.deployment(dep.ID)
	if stored.State != StateRunning {
		t.Fatalf("state = %s, want running (an ambiguously committed write must not be downgraded)", stored.State)
	}
}

// TestDeleteSystemApplicationRefusesWhileDeploying covers the preview teardown
// in-flight guard: a preview with a non-terminal deployment answers ErrConflict
// and no container is removed.
func TestDeleteSystemApplicationRefusesWhileDeploying(t *testing.T) {
	app := testApplication(uuid.New())
	app.IsPreview = true
	repo := &fakeRepository{app: app}
	node := newMockNode()
	// Build the service first so the stale sweep does not fail the row.
	svc := newNodeService(t, repo, node)
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateStarting, ContainerID: node.containerID})

	err := svc.DeleteSystemApplication(context.Background(), app.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict while a deployment is in flight", err)
	}
	if _, err := repo.GetApplication(context.Background(), app.ID); err != nil {
		t.Errorf("the preview must survive a refused teardown: %v", err)
	}
	if node.removeCalls != 0 {
		t.Errorf("removed %v, want no container touched while a deployment is in flight", node.removed)
	}
}

// TestUnresolvedPreviousContainerFailsDeployClosed is the item-5 regression: a
// deployment whose previous container cannot be resolved must not start. The
// pre-Run reconcile removes every app-labelled container, so a blind job with
// an empty previous would take the live release down; the submit fails closed
// and the fresh row is terminal.
func TestUnresolvedPreviousContainerFailsDeployClosed(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	player := newNodeService(t, repo, node)
	// The live release is recorded, but the list that would resolve it fails.
	repo.listDeploymentsErr = errors.New("database unavailable")

	if _, err := player.Deploy(context.Background(), userID, app.ID); err == nil {
		t.Fatal("deploy succeeded although the previous container could not be resolved")
	}
	for _, dep := range repo.deployments {
		if dep.ApplicationID != app.ID {
			continue
		}
		if !dep.State.Terminal() {
			t.Errorf("deployment state = %s, want a terminal failed row (never a blind run)", dep.State)
			continue
		}
		if dep.State != StateFailed {
			t.Errorf("deployment state = %s, want failed", dep.State)
		}
	}
}

// TestEnvReadUsesSingleSnapshotSeam is the item-1 regression: the runtime
// payload must read plain vars and secrets from one snapshot (ListEnvConfig),
// not from two separate collection reads that a concurrent replace can land
// between and drop a key. The store-halves integration test proves the
// snapshot is real; here we pin that the orchestrator takes the seam, and that
// the assembled payload still carries both collections.
func TestEnvReadUsesSingleSnapshotSeam(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t) // plain FOO=bar + secret API_TOKEN
	repo.envVars, repo.secrets = envVars, secrets
	node := newMockNode()
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})
	o.run(context.Background(), job{app: app, dep: dep})

	if stored, _ := repo.deployment(dep.ID); stored.State != StateRunning {
		t.Fatalf("state = %s, want running", stored.State)
	}
	if repo.envConfigCalls != 1 {
		t.Errorf("ListEnvConfig calls = %d, want 1 (the payload must use the single-snapshot seam)", repo.envConfigCalls)
	}
	if node.runCalls != 1 || len(node.requests) != 1 {
		t.Fatalf("run calls = %d, want 1", node.runCalls)
	}
	env := strings.Join(node.requests[0].GetEnv(), "\n")
	for _, want := range []string{"FOO=bar", "API_TOKEN=hunter2"} {
		if !strings.Contains(env, want) {
			t.Errorf("run env = %q, want %q", env, want)
		}
	}
}

// TestPreviousContainerPropagatesError pins the repository seam: a listing
// failure must surface, so callers can fail closed instead of racing the
// reconcile with an empty previous.
func TestPreviousContainerPropagatesError(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	repo.listDeploymentsErr = errors.New("boom")
	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })

	if _, err := svc.previousContainer(context.Background(), app.ID); err == nil {
		t.Fatal("previousContainer hid the ListDeployments error")
	}
}
