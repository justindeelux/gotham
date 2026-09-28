package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testDocument = `services:
  web:
    image: nginx:1.23
    labels:
      gotham.domain: ${DOMAIN}
    environment:
      PASSWORD: ${PASSWORD}
    volumes:
      - data:/data
  worker:
    image: busybox:1.36
volumes:
  data:
`

var testEnv = map[string]string{
	"DOMAIN":   "app.example.com",
	"PASSWORD": "hunter2-secret",
}

// newTestService wires the real domain service over the fakes.
func newTestService(t *testing.T, repo *fakeRepository, agent *fakeAgent) ServiceService {
	t.Helper()
	if repo == nil {
		repo = newFakeRepository()
	}
	return NewService(Config{
		Repository:    repo,
		Logger:        discardLogger(),
		DeployTimeout: 5 * time.Second,
		Dial: func(context.Context, uuid.UUID) (ComposeAgent, error) {
			return agent, nil
		},
	})
}

// createService stores one service for the given owner and returns it.
func createService(t *testing.T, svc ServiceService, repo *fakeRepository, userID uuid.UUID) Service {
	t.Helper()
	serverID := repo.seedServer()
	created, err := svc.Create(context.Background(), userID, CreateRequest{
		Name:        "wordpress",
		ServerID:    serverID,
		ComposeYAML: testDocument,
		Env:         testEnv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

// TestCreateValidation locks the create-time checks.
func TestCreateValidation(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	serverID := repo.seedServer()

	cases := map[string]struct {
		req  CreateRequest
		want error
	}{
		"bad name": {
			req:  CreateRequest{Name: "bad name!", ServerID: serverID, ComposeYAML: testDocument, Env: testEnv},
			want: ErrValidation,
		},
		"missing server": {
			req:  CreateRequest{Name: "ok", ComposeYAML: testDocument, Env: testEnv},
			want: ErrValidation,
		},
		"unknown server": {
			req:  CreateRequest{Name: "ok", ServerID: uuid.New(), ComposeYAML: testDocument, Env: testEnv},
			want: ErrServerNotFound,
		},
		"build context": {
			req:  CreateRequest{Name: "ok", ServerID: serverID, ComposeYAML: "services:\n  web:\n    build: .\n"},
			want: ErrValidation,
		},
		"unresolvable env": {
			req:  CreateRequest{Name: "ok", ServerID: serverID, ComposeYAML: testDocument},
			want: ErrValidation,
		},
		"bad env key": {
			req: CreateRequest{Name: "ok", ServerID: serverID, ComposeYAML: "services:\n  web:\n    image: nginx\n",
				Env: map[string]string{"bad-key": "x"}},
			want: ErrValidation,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), userID, tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create = %v, want %v", err, tc.want)
			}
		})
	}

	created := createService(t, svc, repo, userID)
	if created.Status != StatusCreating || created.Name != "wordpress" || created.UserID != userID {
		t.Fatalf("created = %+v", created)
	}
	if _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", ServerID: created.ServerID, ComposeYAML: testDocument, Env: testEnv,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create = %v, want ErrConflict", err)
	}
}

// TestDeployRendersAndRecords proves a deploy renders the stored document
// (leaving no ${VAR} reference), snapshots it in the deploy row and reports
// the project running.
func TestDeployRendersAndRecords(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	deployed, deploy, err := svc.Deploy(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if deployed.Status != StatusRunning {
		t.Errorf("status = %q, want running", deployed.Status)
	}
	if deploy.State != DeployRunning || deploy.FinishedAt.IsZero() {
		t.Errorf("deploy = %+v", deploy)
	}
	project := ProjectName(created.ID)
	if deploy.ComposeYAML == "" || strings.Contains(deploy.ComposeYAML, "${") {
		t.Errorf("deploy snapshot is not rendered:\n%s", deploy.ComposeYAML)
	}
	if !strings.Contains(deploy.ComposeYAML, "app.example.com") || !strings.Contains(deploy.ComposeYAML, "hunter2-secret") {
		t.Errorf("deploy snapshot lost the rendered values:\n%s", deploy.ComposeYAML)
	}
	if len(agent.validated) != 1 || agent.validated[0] != project {
		t.Errorf("validated = %v, want %s", agent.validated, project)
	}
	up := agent.lastUp(t)
	if up.project != project || up.restart {
		t.Errorf("up = %+v", up)
	}
	if up.yaml != deploy.ComposeYAML {
		t.Errorf("the node ran a different document than the recorded snapshot")
	}

	// The printed service maps the domain label into a route.
	domains, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(domains.Env) != 2 {
		t.Errorf("env = %+v", domains.Env)
	}
}

// TestDeployFailureRedactsEnvironment proves a failed agent call is recorded
// and returned with the environment value redacted, and that the deploy
// failure only flips the service status when the node actually changed.
func TestDeployFailureRedactsEnvironment(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{validateErr: fmt.Errorf("%w: validate: cannot resolve ${PASSWORD}", ErrValidation)}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	_, _, err := svc.Deploy(context.Background(), userID, created.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Deploy = %v, want ErrValidation", err)
	}
	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusCreating {
		t.Errorf("a validation failure before any node change must keep the status, got %q", after.Status)
	}
	deploys, err := svc.Deploys(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Deploys: %v", err)
	}
	if len(deploys) != 1 || deploys[0].State != DeployFailed {
		t.Fatalf("deploys = %+v", deploys)
	}

	// An up failure is a node change: the service goes to error and the
	// message is redacted everywhere it is stored.
	agent.validateErr = nil
	agent.upErr = fmt.Errorf("%w: up: invalid value hunter2-secret", ErrDeployFailed)
	_, _, err = svc.Deploy(context.Background(), userID, created.ID)
	if !errors.Is(err, ErrDeployFailed) {
		t.Fatalf("Deploy(up) = %v, want ErrDeployFailed", err)
	}
	if strings.Contains(err.Error(), "hunter2-secret") {
		t.Fatalf("returned error leaked the value: %v", err)
	}
	after, err = svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusError {
		t.Errorf("status after a failed up = %q, want error", after.Status)
	}
	deploys, _ = svc.Deploys(context.Background(), userID, created.ID)
	if strings.Contains(deploys[0].Error, "hunter2-secret") || !strings.Contains(deploys[0].Error, "<redacted>") {
		t.Errorf("stored deploy error = %q, want the value redacted", deploys[0].Error)
	}
}

// TestStopAndRestart proves stop runs a down and restart uses the restart verb
// only for a running project.
func TestStopAndRestart(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	// Restart of a never-deployed service starts it instead.
	restarted, err := svc.Restart(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.Status != StatusRunning {
		t.Errorf("status = %q, want running", restarted.Status)
	}
	if agent.lastUp(t).restart {
		t.Errorf("a stopped service must be started, not restarted")
	}

	stopped, err := svc.Stop(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != StatusStopped {
		t.Errorf("status = %q, want stopped", stopped.Status)
	}
	if len(agent.downs) != 1 || agent.downs[0] != ProjectName(created.ID) {
		t.Errorf("downs = %v", agent.downs)
	}

	// A running service restarts in place.
	repo.mu.Lock()
	running := repo.services[created.ID]
	running.Status = StatusRunning
	repo.services[created.ID] = running
	repo.mu.Unlock()
	if _, err := svc.Restart(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Restart(running): %v", err)
	}
	if !agent.lastUp(t).restart {
		t.Errorf("a running service must be restarted in place")
	}
}

// TestDeleteStopsThenSoftDeletes proves delete brings the project down and
// keeps the row when the node cannot be reached, and that a document which no
// longer renders does not make the service undeletable.
func TestDeleteStopsThenSoftDeletes(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	agent.downErr = fmt.Errorf("%w: down: node unreachable", ErrAgentUnavailable)
	if err := svc.Delete(context.Background(), userID, created.ID); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("Delete = %v, want ErrAgentUnavailable", err)
	}
	if _, err := svc.Get(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("the row must survive a failed delete: %v", err)
	}

	agent.downErr = nil
	if err := svc.Delete(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(context.Background(), userID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}

	// A document whose environment no longer resolves deletes anyway: the
	// rename + env removal in one patch makes the stored document
	// unrenderable.
	broken := createService(t, svc, repo, userID)
	broken.Env = map[string]string{}
	repo.mu.Lock()
	repo.services[broken.ID] = broken
	repo.mu.Unlock()
	if err := svc.Delete(context.Background(), userID, broken.ID); err != nil {
		t.Fatalf("Delete(unrenderable) = %v", err)
	}
	if _, err := svc.Get(context.Background(), userID, broken.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

// TestOwnershipAndLogs proves IDs cannot be probed across users and that a log
// read validates the compose service name and delegates to the agent.
func TestOwnershipAndLogs(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{chunks: [][]byte{[]byte("alive\n")}}
	svc := newTestService(t, repo, agent)
	ownerID := uuid.New()
	created := createService(t, svc, repo, ownerID)
	otherID := uuid.New()

	if _, err := svc.Get(context.Background(), otherID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(other user) = %v, want ErrNotFound", err)
	}
	if _, _, err := svc.Deploy(context.Background(), otherID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Deploy(other user) = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(context.Background(), otherID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(other user) = %v, want ErrNotFound", err)
	}

	chunks, err := svc.Logs(context.Background(), ownerID, created.ID, "worker", 25, true)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if got := string(<-chunks); got != "alive\n" {
		t.Errorf("chunk = %q", got)
	}
	if len(agent.logCalls) != 1 || agent.logCalls[0].service != "worker" ||
		agent.logCalls[0].tail != 25 || !agent.logCalls[0].follow {
		t.Errorf("log calls = %+v", agent.logCalls)
	}
	if _, err := svc.Logs(context.Background(), ownerID, created.ID, "bad service", 0, false); !errors.Is(err, ErrValidation) {
		t.Errorf("Logs(bad service) = %v, want ErrValidation", err)
	}
	if _, err := svc.Logs(context.Background(), ownerID, created.ID, "", -1, false); !errors.Is(err, ErrValidation) {
		t.Errorf("Logs(negative tail) = %v, want ErrValidation", err)
	}

	containers, err := svc.Containers(context.Background(), ownerID, created.ID)
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	if len(containers) != 1 || containers[0].Service != "web" {
		t.Errorf("containers = %+v", containers)
	}
	if len(agent.psCalls) != 1 || agent.psCalls[0] != ProjectName(created.ID) {
		t.Errorf("ps calls = %v", agent.psCalls)
	}
}

// TestUpdateRevalidates proves a patch that breaks rendering is rejected and a
// valid one is persisted.
func TestUpdateRevalidates(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	broken := "services:\n  web:\n    image: nginx\n    environment:\n      X: ${GONE}\n"
	if _, err := svc.Update(context.Background(), userID, created.ID, UpdateRequest{ComposeYAML: &broken}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Update(broken) = %v, want ErrValidation", err)
	}
	name := "renamed"
	updated, err := svc.Update(context.Background(), userID, created.ID, UpdateRequest{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "renamed" || updated.ComposeYAML != testDocument {
		t.Errorf("updated = %+v", updated)
	}
}

// TestFeatureFlag proves the kill switch disables construction and mutation.
func TestFeatureFlag(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	if Enabled() {
		t.Fatal("Enabled() = true with FEATURE_SERVICES=false")
	}
	if svc := NewDefaultService(Config{
		Repository: newFakeRepository(),
		Dial:       func(context.Context, uuid.UUID) (ComposeAgent, error) { return &fakeAgent{}, nil },
	}); svc != nil {
		t.Fatalf("NewDefaultService = %v, want nil", svc)
	}
	svc := newTestService(t, newFakeRepository(), &fakeAgent{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Create = %v, want ErrDisabled", err)
	}
}
