package deploy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type recordingNotifier struct {
	mu        sync.Mutex
	recorded  []DeployResult
	redeploys []RedeployResult
}

var _ Notifier = (*recordingNotifier)(nil)

func (n *recordingNotifier) DeployFinished(_ context.Context, result DeployResult) {
	n.mu.Lock()
	n.recorded = append(n.recorded, result)
	n.mu.Unlock()
}

func (n *recordingNotifier) RedeployNeeded(_ context.Context, result RedeployResult) {
	n.mu.Lock()
	n.redeploys = append(n.redeploys, result)
	n.mu.Unlock()
}

func (n *recordingNotifier) redeployResults() []RedeployResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]RedeployResult(nil), n.redeploys...)
}

func (n *recordingNotifier) results() []DeployResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]DeployResult(nil), n.recorded...)
}

func TestOrchestratorNotifiesSuccess(t *testing.T) {
	app := testApplication(uuid.New())
	app.PrimaryDomain = &ApplicationDomain{Domain: "demo.example.com", Port: 3000, Protocol: ProtocolHTTP}
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
	if result.Host != app.PrimaryDomain.Domain {
		t.Errorf("host = %q, want %q", result.Host, app.PrimaryDomain.Domain)
	}
	if result.Error != "" {
		t.Errorf("error = %q, want empty on success", result.Error)
	}
	if result.FinishedAt.IsZero() {
		t.Error("finished_at is not set on the result")
	}
}

func TestOrchestratorNotifiesFailure(t *testing.T) {
	app := testApplication(uuid.New())
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
}

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
