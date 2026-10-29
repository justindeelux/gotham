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

func newTestService(t *testing.T, repo *fakeRepository, agent *fakeAgent) ServiceService {
	t.Helper()
	if repo == nil {
		repo = newFakeRepository()
	}
	svc := NewService(Config{
		Repository:    repo,
		Logger:        discardLogger(),
		DeployTimeout: 5 * time.Second,
		Dial: func(context.Context, uuid.UUID) (ComposeAgent, error) {
			return agent, nil
		},
	})
	t.Cleanup(func() { closeTestService(t, svc) })
	return svc
}

func closeTestService(t *testing.T, svc ServiceService) {
	t.Helper()
	if closer, ok := svc.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
}

// waitForDeploy waits for the newest deploy of a service to leave deploying.
func waitForDeploy(t *testing.T, svc ServiceService, userID, serviceID uuid.UUID) Deploy {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		deploys, err := svc.Deploys(context.Background(), userID, serviceID)
		if err != nil {
			t.Fatalf("Deploys: %v", err)
		}
		if len(deploys) > 0 && deploys[0].State != DeployDeploying {
			return deploys[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the deploy never reached a terminal state")
	return Deploy{}
}

func createService(t *testing.T, svc ServiceService, repo *fakeRepository, userID uuid.UUID) Service {
	t.Helper()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	created, err := svc.Create(context.Background(), userID, CreateRequest{
		Name:          "wordpress",
		EnvironmentID: envID,
		ServerID:      serverID,
		ComposeYAML:   testDocument,
		Env:           testEnv,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func TestCreateValidation(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{})
	userID := uuid.New()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()

	cases := map[string]struct {
		req  CreateRequest
		want error
	}{
		"bad name": {
			req:  CreateRequest{Name: "bad name!", EnvironmentID: envID, ServerID: serverID, ComposeYAML: testDocument, Env: testEnv},
			want: ErrValidation,
		},
		"missing environment": {
			req:  CreateRequest{Name: "ok", ServerID: serverID, ComposeYAML: testDocument, Env: testEnv},
			want: ErrValidation,
		},
		"missing server": {
			req:  CreateRequest{Name: "ok", EnvironmentID: envID, ComposeYAML: testDocument, Env: testEnv},
			want: ErrValidation,
		},
		"unknown server": {
			req:  CreateRequest{Name: "ok", EnvironmentID: envID, ServerID: uuid.New(), ComposeYAML: testDocument, Env: testEnv},
			want: ErrServerNotFound,
		},
		"build context": {
			req:  CreateRequest{Name: "ok", EnvironmentID: envID, ServerID: serverID, ComposeYAML: "services:\n  web:\n    build: .\n"},
			want: ErrValidation,
		},
		"unresolvable env": {
			req:  CreateRequest{Name: "ok", EnvironmentID: envID, ServerID: serverID, ComposeYAML: testDocument},
			want: ErrValidation,
		},
		"bad env key": {
			req: CreateRequest{Name: "ok", EnvironmentID: envID, ServerID: serverID, ComposeYAML: "services:\n  web:\n    image: nginx\n",
				Env: map[string]string{"bad-key": "x"}},
			want: ErrValidation,
		},
		"unknown template": {
			req:  CreateRequest{Name: "ok", EnvironmentID: envID, ServerID: serverID, ComposeYAML: testDocument, Env: testEnv, TemplateSlug: "nope"},
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
		Name: "wordpress", EnvironmentID: created.EnvironmentID, ServerID: created.ServerID, ComposeYAML: testDocument, Env: testEnv,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create = %v, want ErrConflict", err)
	}
}

func TestCreateStoresTemplateSlug(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(Config{
		Repository: repo,
		Logger:     discardLogger(),
		TemplateExists: func(slug string) bool {
			return slug == "wordpress"
		},
	})
	userID := uuid.New()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()

	created, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "wordpress", EnvironmentID: envID, ServerID: serverID,
		ComposeYAML: testDocument, Env: testEnv, TemplateSlug: "wordpress",
	})
	if err != nil {
		t.Fatalf("Create(template): %v", err)
	}
	if created.TemplateSlug != "wordpress" {
		t.Errorf("template = %q, want wordpress", created.TemplateSlug)
	}
	if NewServiceResponse(created, false).Template != "wordpress" {
		t.Errorf("response template = %q", NewServiceResponse(created, false).Template)
	}

	imported := createService(t, svc, repo, userID)
	if imported.TemplateSlug != "" {
		t.Errorf("compose import template = %q, want empty", imported.TemplateSlug)
	}
	if NewServiceResponse(imported, false).Template != "" {
		t.Errorf("compose import must not report a template")
	}
}

func TestDeployRendersAndRecords(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	started, deploy, err := svc.Deploy(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if started.Status != StatusCreating {
		t.Errorf("status right after 202 = %q, want the unchanged creating", started.Status)
	}
	if deploy.State != DeployDeploying || !deploy.FinishedAt.IsZero() {
		t.Errorf("deploy = %+v, want a pending deploy", deploy)
	}
	finished := waitForDeploy(t, svc, userID, created.ID)
	if finished.State != DeployRunning || finished.FinishedAt.IsZero() {
		t.Errorf("deploy = %+v", finished)
	}
	deployed, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if deployed.Status != StatusRunning {
		t.Errorf("status = %q, want running", deployed.Status)
	}
	project := ProjectName(created.ID)
	if finished.ComposeYAML == "" || strings.Contains(finished.ComposeYAML, "${") {
		t.Errorf("deploy snapshot is not rendered:\n%s", finished.ComposeYAML)
	}
	if !strings.Contains(finished.ComposeYAML, "app.example.com") || !strings.Contains(finished.ComposeYAML, "hunter2-secret") {
		t.Errorf("deploy snapshot lost the rendered values:\n%s", finished.ComposeYAML)
	}
	if len(agent.validated) != 1 || agent.validated[0] != project {
		t.Errorf("validated = %v, want %s", agent.validated, project)
	}
	up := agent.lastUp(t)
	if up.project != project || up.restart {
		t.Errorf("up = %+v", up)
	}
	if up.yaml != finished.ComposeYAML {
		t.Errorf("the node ran a different document than the recorded snapshot")
	}

	domains, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(domains.Env) != 2 {
		t.Errorf("env = %+v", domains.Env)
	}
}

func TestDeployFailureRedactsEnvironment(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{validateErr: fmt.Errorf("%w: validate: cannot resolve ${PASSWORD}", ErrValidation)}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	if _, _, err := svc.Deploy(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Deploy must accept the attempt and fail in the background: %v", err)
	}
	failed := waitForDeploy(t, svc, userID, created.ID)
	if failed.State != DeployFailed {
		t.Fatalf("deploy = %+v, want failed", failed)
	}
	if !strings.Contains(failed.Error, "cannot resolve") {
		t.Fatalf("stored deploy error = %q, want the validation reason", failed.Error)
	}
	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusError {
		t.Errorf("status after a failed validate = %q, want error", after.Status)
	}

	agent.mu.Lock()
	agent.validateErr = nil
	agent.upErr = fmt.Errorf("%w: up: invalid value hunter2-secret", ErrDeployFailed)
	agent.mu.Unlock()
	if _, _, err := svc.Deploy(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Deploy(up): %v", err)
	}
	failed = waitForDeploy(t, svc, userID, created.ID)
	if failed.State != DeployFailed {
		t.Fatalf("deploy = %+v, want failed", failed)
	}
	if !strings.Contains(failed.Error, "<redacted>") || strings.Contains(failed.Error, "hunter2-secret") {
		t.Fatalf("stored deploy error = %q, want the value redacted", failed.Error)
	}
	after, err = svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusError {
		t.Errorf("status after a failed up = %q, want error", after.Status)
	}
}

func TestStopAndRestart(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

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
	if len(agent.downs) != 1 || agent.downs[0].project != ProjectName(created.ID) {
		t.Errorf("downs = %v", agent.downs)
	}

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

	broken := createService(t, svc, repo, userID)
	broken.Env = map[string]string{}
	repo.mu.Lock()
	repo.services[broken.ID] = broken
	repo.mu.Unlock()
	if err := svc.Delete(context.Background(), userID, broken.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("Delete(unrenderable, no snapshot) = %v, want ErrValidation", err)
	}
	if _, err := svc.Get(context.Background(), userID, broken.ID); err != nil {
		t.Fatalf("the row must survive an unconfirmed shutdown: %v", err)
	}

	snapshot := "services:\n  web:\n    image: nginx:1.23\n    environment:\n      DOMAIN: app.example.com\n"
	if _, err := repo.CreateServiceDeploy(context.Background(), Deploy{
		ID: uuid.New(), ServiceID: broken.ID, State: DeployRunning, ComposeYAML: snapshot,
	}); err != nil {
		t.Fatalf("CreateServiceDeploy: %v", err)
	}
	if err := svc.Delete(context.Background(), userID, broken.ID); err != nil {
		t.Fatalf("Delete(snapshot fallback) = %v", err)
	}
	if _, err := svc.Get(context.Background(), userID, broken.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}

	agent.mu.Lock()
	lastDown := agent.downs[len(agent.downs)-1]
	agent.mu.Unlock()
	if lastDown.project != ProjectName(broken.ID) || lastDown.yaml != snapshot {
		t.Errorf("last down = %+v, want the running deploy snapshot", lastDown)
	}
}

func TestDeployDoesNotClobberConcurrentEdit(t *testing.T) {
	repo := newFakeRepository()
	gate := make(chan struct{})
	agent := &fakeAgent{upGate: gate}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	if _, _, err := svc.Deploy(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForUp(t, agent)

	updateDone := make(chan error, 1)
	go func() {
		name := "updated-during-deploy"
		_, err := svc.Update(context.Background(), userID, created.ID, UpdateRequest{Name: &name})
		updateDone <- err
	}()
	select {
	case err := <-updateDone:
		t.Fatalf("Update completed during a deploy: %v (want it to wait for the lifecycle lock)", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(gate)
	if deploy := waitForDeploy(t, svc, userID, created.ID); deploy.State != DeployRunning {
		t.Fatalf("deploy = %+v, want running", deploy)
	}
	if err := <-updateDone; err != nil {
		t.Fatalf("Update after deploy: %v", err)
	}

	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Name != "updated-during-deploy" {
		t.Fatalf("the concurrent rename was overwritten by the deploy completion: %q", after.Name)
	}
	if after.Status != StatusRunning {
		t.Fatalf("status = %q, want running", after.Status)
	}
}

func TestCloseRecordsAnInFlightDeployAsFailed(t *testing.T) {
	repo := newFakeRepository()
	gate := make(chan struct{})
	agent := &fakeAgent{upGate: gate}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	if _, _, err := svc.Deploy(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForUp(t, agent)
	closeTestService(t, svc)

	deploys, err := svc.Deploys(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Deploys: %v", err)
	}
	if len(deploys) != 1 || deploys[0].State != DeployFailed {
		t.Fatalf("deploys = %+v, want a failed row after shutdown", deploys)
	}
	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusError {
		t.Fatalf("status = %q, want error", after.Status)
	}
}

func TestLifecycleActionsConflictWhileDeploying(t *testing.T) {
	repo := newFakeRepository()
	gate := make(chan struct{})
	agent := &fakeAgent{upGate: gate}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	if _, _, err := svc.Deploy(context.Background(), userID, created.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	waitForUp(t, agent)

	if _, err := svc.Stop(context.Background(), userID, created.ID); !errors.Is(err, ErrDeployInFlight) {
		t.Errorf("Stop during a deploy = %v, want ErrDeployInFlight", err)
	}
	if _, err := svc.Restart(context.Background(), userID, created.ID); !errors.Is(err, ErrDeployInFlight) {
		t.Errorf("Restart during a deploy = %v, want ErrDeployInFlight", err)
	}
	if err := svc.Delete(context.Background(), userID, created.ID); !errors.Is(err, ErrDeployInFlight) {
		t.Errorf("Delete during a deploy = %v, want ErrDeployInFlight", err)
	}

	close(gate)
	if deploy := waitForDeploy(t, svc, userID, created.ID); deploy.State != DeployRunning {
		t.Fatalf("deploy = %+v, want running", deploy)
	}
	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusRunning {
		t.Fatalf("final status = %q, want running", after.Status)
	}
}

func TestRecoverInterruptedDeploys(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{}).(*service)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)
	if _, err := repo.CreateServiceDeploy(context.Background(), Deploy{
		ID: uuid.New(), ServiceID: created.ID, State: DeployDeploying, ComposeYAML: "services: {}",
	}); err != nil {
		t.Fatalf("CreateServiceDeploy: %v", err)
	}

	svc.recoverInterruptedDeploys()

	deploys, err := svc.Deploys(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Deploys: %v", err)
	}
	if len(deploys) != 1 || deploys[0].State != DeployFailed || deploys[0].Error != interruptedDeployMessage {
		t.Fatalf("deploys = %+v, want one interrupted failure", deploys)
	}
	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusError {
		t.Fatalf("status = %q, want error", after.Status)
	}
}

func TestRecoverKeepsARunningStatusWhenAnInterruptedDeployWasARedeploy(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, &fakeAgent{}).(*service)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)
	if _, err := repo.UpdateServiceStatus(context.Background(), created.ID, StatusRunning); err != nil {
		t.Fatalf("UpdateServiceStatus: %v", err)
	}
	if _, err := repo.CreateServiceDeploy(context.Background(), Deploy{
		ID: uuid.New(), ServiceID: created.ID, State: DeployDeploying, ComposeYAML: "services: {}",
	}); err != nil {
		t.Fatalf("CreateServiceDeploy: %v", err)
	}

	svc.recoverInterruptedDeploys()

	after, err := svc.Get(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != StatusRunning {
		t.Fatalf("status = %q, want the running project left alone", after.Status)
	}
	deploys, _ := svc.Deploys(context.Background(), userID, created.ID)
	if deploys[0].State != DeployFailed {
		t.Fatalf("deploy = %+v, want failed", deploys[0])
	}
}

func waitForDown(t *testing.T, agent *fakeAgent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		calls := len(agent.downs)
		agent.mu.Unlock()
		if calls > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the operation never reached the agent's Down")
}

func TestQueuedRestartAfterDeleteDoesNotResurrect(t *testing.T) {
	repo := newFakeRepository()
	gate := make(chan struct{})
	agent := &fakeAgent{downGate: gate}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- svc.Delete(context.Background(), userID, created.ID) }()
	waitForDown(t, agent)

	restartDone := make(chan error, 1)
	go func() {
		_, err := svc.Restart(context.Background(), userID, created.ID)
		restartDone <- err
	}()
	time.Sleep(200 * time.Millisecond)
	close(gate)

	if err := <-deleteDone; err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := <-restartDone; !errors.Is(err, ErrNotFound) {
		t.Fatalf("queued Restart = %v, want ErrNotFound", err)
	}
	agent.mu.Lock()
	ups := len(agent.ups)
	agent.mu.Unlock()
	if ups != 0 {
		t.Fatalf("the queued restart resurrected the deleted project (%d Up calls)", ups)
	}
}

func TestQueuedDeployAfterDeleteDoesNotResurrect(t *testing.T) {
	repo := newFakeRepository()
	gate := make(chan struct{})
	agent := &fakeAgent{downGate: gate}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- svc.Delete(context.Background(), userID, created.ID) }()
	waitForDown(t, agent)

	deployDone := make(chan error, 1)
	go func() {
		_, _, err := svc.Deploy(context.Background(), userID, created.ID)
		deployDone <- err
	}()
	time.Sleep(200 * time.Millisecond)
	close(gate)

	if err := <-deleteDone; err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := <-deployDone; !errors.Is(err, ErrNotFound) {
		t.Fatalf("queued Deploy = %v, want ErrNotFound", err)
	}
	agent.mu.Lock()
	ups := len(agent.ups)
	agent.mu.Unlock()
	if ups != 0 {
		t.Fatalf("the queued deploy resurrected the deleted project (%d Up calls)", ups)
	}
	deploys, err := repo.ListServiceDeploys(context.Background(), created.ID, 10)
	if err != nil {
		t.Fatalf("ListServiceDeploys: %v", err)
	}
	if len(deploys) != 0 {
		t.Fatalf("the queued deploy recorded attempts for a deleted service: %+v", deploys)
	}
}

func TestTerminalLogErrorIsRedacted(t *testing.T) {
	secret := testEnv["PASSWORD"]
	repo := newFakeRepository()
	agent := &fakeAgent{
		chunks:    [][]byte{[]byte("partial output\n")},
		streamErr: fmt.Errorf("%w: logs: invalid value %s", ErrDeployFailed, secret),
	}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	stream, err := svc.Logs(context.Background(), userID, created.ID, "worker", 10, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	var body strings.Builder
	for chunk := range stream.Chunks() {
		body.Write(chunk)
	}
	if body.String() != "partial output\n" {
		t.Fatalf("log content = %q, want it untouched", body.String())
	}
	terminal := stream.Err()
	if !errors.Is(terminal, ErrDeployFailed) {
		t.Fatalf("terminal error = %v, want ErrDeployFailed", terminal)
	}
	if strings.Contains(terminal.Error(), secret) {
		t.Fatalf("terminal error leaked the environment value: %v", terminal)
	}
	if !strings.Contains(terminal.Error(), "<redacted>") {
		t.Fatalf("terminal error does not show the redaction: %v", terminal)
	}
}

func TestLogsRejectsUnknownComposeServiceBeforeDialing(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	dials := 0
	svc := NewService(Config{
		Repository: repo,
		Logger:     discardLogger(),
		Dial: func(context.Context, uuid.UUID) (ComposeAgent, error) {
			dials++
			return agent, nil
		},
	})
	userID := uuid.New()
	created := createService(t, svc, repo, userID)

	if _, err := svc.Logs(context.Background(), userID, created.ID, "missing", 10, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("Logs(unknown) = %v, want ErrValidation", err)
	}
	if dials != 0 {
		t.Fatalf("an unknown selector reached the node (%d dials)", dials)
	}
	stream, err := svc.Logs(context.Background(), userID, created.ID, "web", 10, false)
	if err != nil {
		t.Fatalf("Logs(web): %v", err)
	}
	for range stream.Chunks() {
	}
	if dials != 1 {
		t.Fatalf("a declared selector dialed %d times, want 1", dials)
	}
}

func waitForUp(t *testing.T, agent *fakeAgent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		calls := len(agent.ups)
		agent.mu.Unlock()
		if calls > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the deploy never reached the agent")
}

func TestNodeOperationErrorsRedactEnvironment(t *testing.T) {
	secret := testEnv["PASSWORD"]
	cases := map[string]struct {
		prime func(*fakeAgent)
		call  func(ServiceService, uuid.UUID, uuid.UUID) error
		want  error
	}{
		"stop": {
			prime: func(a *fakeAgent) { a.downErr = fmt.Errorf("%w: down: bad %s", ErrDeployFailed, secret) },
			call: func(svc ServiceService, userID, id uuid.UUID) error {
				_, err := svc.Stop(context.Background(), userID, id)
				return err
			},
			want: ErrDeployFailed,
		},
		"delete": {
			prime: func(a *fakeAgent) { a.downErr = fmt.Errorf("%w: down: bad %s", ErrDeployFailed, secret) },
			call: func(svc ServiceService, userID, id uuid.UUID) error {
				return svc.Delete(context.Background(), userID, id)
			},
			want: ErrDeployFailed,
		},
		"restart": {
			prime: func(a *fakeAgent) { a.upErr = fmt.Errorf("%w: up: bad %s", ErrDeployFailed, secret) },
			call: func(svc ServiceService, userID, id uuid.UUID) error {
				_, err := svc.Restart(context.Background(), userID, id)
				return err
			},
			want: ErrDeployFailed,
		},
		"containers": {
			prime: func(a *fakeAgent) { a.psErr = fmt.Errorf("%w: ps: bad %s", ErrDeployFailed, secret) },
			call: func(svc ServiceService, userID, id uuid.UUID) error {
				_, err := svc.Containers(context.Background(), userID, id)
				return err
			},
			want: ErrDeployFailed,
		},
		"logs": {
			prime: func(a *fakeAgent) { a.logsErr = fmt.Errorf("%w: logs: bad %s", ErrDeployFailed, secret) },
			call: func(svc ServiceService, userID, id uuid.UUID) error {
				_, err := svc.Logs(context.Background(), userID, id, "", 10, false)
				return err
			},
			want: ErrDeployFailed,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepository()
			agent := &fakeAgent{}
			tc.prime(agent)
			svc := newTestService(t, repo, agent)
			userID := uuid.New()
			created := createService(t, svc, repo, userID)

			err := tc.call(svc, userID, created.ID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("the error leaked the environment value: %v", err)
			}
			if !strings.Contains(err.Error(), "<redacted>") {
				t.Fatalf("the error does not show the redaction: %v", err)
			}
		})
	}
}

func TestAgentConnectionsAreClosed(t *testing.T) {
	steps := []struct {
		name string
		call func(*testing.T, ServiceService, uuid.UUID, uuid.UUID)
	}{
		{"deploy", func(t *testing.T, svc ServiceService, userID, id uuid.UUID) {
			if _, _, err := svc.Deploy(context.Background(), userID, id); err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			waitForDeploy(t, svc, userID, id)
		}},
		{"stop", func(t *testing.T, svc ServiceService, userID, id uuid.UUID) {
			if _, err := svc.Stop(context.Background(), userID, id); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		}},
		{"restart", func(t *testing.T, svc ServiceService, userID, id uuid.UUID) {
			if _, err := svc.Restart(context.Background(), userID, id); err != nil {
				t.Fatalf("Restart: %v", err)
			}
		}},
		{"containers", func(t *testing.T, svc ServiceService, userID, id uuid.UUID) {
			if _, err := svc.Containers(context.Background(), userID, id); err != nil {
				t.Fatalf("Containers: %v", err)
			}
		}},
		{"delete", func(t *testing.T, svc ServiceService, userID, id uuid.UUID) {
			if err := svc.Delete(context.Background(), userID, id); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			repo := newFakeRepository()
			agent := &fakeAgent{chunks: [][]byte{[]byte("line\n")}}
			svc := newTestService(t, repo, agent)
			userID := uuid.New()
			created := createService(t, svc, repo, userID)

			before := agent.currentCloses()
			step.call(t, svc, userID, created.ID)
			if after := agent.currentCloses(); after <= before {
				t.Fatalf("the %s operation did not close its client (%d -> %d)", step.name, before, after)
			}
		})
	}

	repo := newFakeRepository()
	agent := &fakeAgent{chunks: [][]byte{[]byte("line\n")}}
	svc := newTestService(t, repo, agent)
	userID := uuid.New()
	created := createService(t, svc, repo, userID)
	before := agent.currentCloses()
	stream, err := svc.Logs(context.Background(), userID, created.ID, "worker", 10, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	for range stream.Chunks() {
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close: %v", err)
	}
	if after := agent.currentCloses(); after <= before {
		t.Fatalf("the log stream did not close its client (%d -> %d)", before, after)
	}
}

func TestLifecycleChangesResyncTheProxy(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{}
	proxySync := &fakeRouteSync{}
	svc := NewService(Config{
		Repository:    repo,
		Logger:        discardLogger(),
		DeployTimeout: 5 * time.Second,
		Dial:          func(context.Context, uuid.UUID) (ComposeAgent, error) { return agent, nil },
		Proxy:         proxySync,
	})
	userID := uuid.New()
	created := createService(t, svc, repo, userID)
	t.Cleanup(func() { closeTestService(t, svc) })

	for _, step := range []struct {
		name string
		call func() error
	}{
		{"deploy", func() error {
			if _, _, err := svc.Deploy(context.Background(), userID, created.ID); err != nil {
				return err
			}
			waitForDeploy(t, svc, userID, created.ID)
			for i := 0; i < 100 && len(proxySync.calls()) == 0; i++ {
				time.Sleep(10 * time.Millisecond)
			}
			return nil
		}},
		{"stop", func() error {
			_, err := svc.Stop(context.Background(), userID, created.ID)
			return err
		}},
		{"restart", func() error {
			_, err := svc.Restart(context.Background(), userID, created.ID)
			return err
		}},
		{"delete", func() error { return svc.Delete(context.Background(), userID, created.ID) }},
	} {
		if err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}
	calls := proxySync.calls()
	if len(calls) != 4 {
		t.Fatalf("proxy resyncs = %v, want one per lifecycle change", calls)
	}
	for _, serverID := range calls {
		if serverID != created.ServerID {
			t.Fatalf("resync target = %s, want %s", serverID, created.ServerID)
		}
	}

	proxySync.err = fmt.Errorf("proxy down")
	repo2 := newFakeRepository()
	svc2 := NewService(Config{
		Repository: repo2,
		Logger:     discardLogger(),
		Dial:       func(context.Context, uuid.UUID) (ComposeAgent, error) { return agent, nil },
		Proxy:      proxySync,
	})
	second := createService(t, svc2, repo2, userID)
	t.Cleanup(func() { closeTestService(t, svc2) })
	if _, _, err := svc2.Deploy(context.Background(), userID, second.ID); err != nil {
		t.Fatalf("a failed resync must not fail the deploy: %v", err)
	}
}

func TestOwnershipAndLogs(t *testing.T) {
	repo := newFakeRepository()
	agent := &fakeAgent{chunks: [][]byte{[]byte("alive\n")}}
	svc := newTestService(t, repo, agent)
	ownerID := uuid.New()
	created := createService(t, svc, repo, ownerID)

	stream, err := svc.Logs(context.Background(), ownerID, created.ID, "worker", 25, true)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if got := string(<-stream.Chunks()); got != "alive\n" {
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
