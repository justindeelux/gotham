package deploy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// recordingNotifier captures the terminal results handed to the deploy hook.
type recordingNotifier struct {
	mu       sync.Mutex
	recorded []DeployResult
}

// Compile-time guarantee.
var _ Notifier = (*recordingNotifier)(nil)

// DeployFinished implements Notifier. It records synchronously, which is what
// lets a test assert right after the run returns; the production
// implementation queues the delivery instead.
func (n *recordingNotifier) DeployFinished(_ context.Context, result DeployResult) {
	n.mu.Lock()
	n.recorded = append(n.recorded, result)
	n.mu.Unlock()
}

// results returns the recorded outcomes.
func (n *recordingNotifier) results() []DeployResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]DeployResult(nil), n.recorded...)
}

// TestOrchestratorNotifiesSuccess proves a deployment reaching running calls
// the hook exactly once with the application, team and host.
func TestOrchestratorNotifiesSuccess(t *testing.T) {
	app := testApplication(uuid.New())
	app.TeamID = uuid.New()
	app.BaseDomain = "demo.example.com"
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	notifier := &recordingNotifier{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(newMockNode()),
		Emitter:    NewEmitter(&recordPublisher{}),
		Notifier:   notifier,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	results := notifier.results()
	if len(results) != 1 {
		t.Fatalf("notifications = %d, want exactly 1 per terminal transition", len(results))
	}
	result := results[0]
	if result.State != StateRunning {
		t.Errorf("state = %s, want running", result.State)
	}
	if result.Application != app.Name || result.ApplicationID != app.ID {
		t.Errorf("application = %q/%s, want %q/%s", result.Application, result.ApplicationID, app.Name, app.ID)
	}
	if result.TeamID != app.TeamID {
		t.Errorf("team = %s, want %s", result.TeamID, app.TeamID)
	}
	if result.Host != app.BaseDomain {
		t.Errorf("host = %q, want %q", result.Host, app.BaseDomain)
	}
	if result.Error != "" {
		t.Errorf("error = %q, want empty on success", result.Error)
	}
	if result.FinishedAt.IsZero() {
		t.Error("finished_at is not set on the result")
	}
}

// TestOrchestratorNotifiesFailure proves a failed deployment calls the hook
// exactly once with the failure text.
func TestOrchestratorNotifiesFailure(t *testing.T) {
	app := testApplication(uuid.New())
	app.TeamID = uuid.New()
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	notifier := &recordingNotifier{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{err: errors.New("clone boom")},
		Dial:       dialAlways(newMockNode()),
		Notifier:   notifier,
	})

	o.run(context.Background(), job{app: app, dep: dep})

	results := notifier.results()
	if len(results) != 1 {
		t.Fatalf("notifications = %d, want exactly 1", len(results))
	}
	result := results[0]
	if result.State != StateFailed {
		t.Errorf("state = %s, want failed", result.State)
	}
	if !strings.Contains(result.Error, "clone boom") {
		t.Errorf("error = %q, want the failure cause", result.Error)
	}
	if result.TeamID != app.TeamID {
		t.Errorf("team = %s, want %s", result.TeamID, app.TeamID)
	}
}

// TestOrchestratorWithoutNotifierIsSafe pins the nil (flag-off) hook path: the
// state machine still runs and no hook is required.
func TestOrchestratorWithoutNotifierIsSafe(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(newMockNode()),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok || stored.State != StateRunning {
		t.Fatalf("state = %v (%v), want running with a nil notifier", stored.State, ok)
	}
}
