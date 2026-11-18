package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

const composeControlDocument = `services:
  web:
    image: example.com/app:1.0
    depends_on:
      - worker
  worker:
    image: example.com/worker:1.0
    depends_on:
      db:
        condition: service_started
  db:
    image: postgres:16
`

func seededComposeProject(t *testing.T) (*fakeRepository, *mockNode, Application, Deployment) {
	t.Helper()
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{
		Kind:            KindDeploy,
		State:           StateRunning,
		ContainerID:     "web-id",
		ComposeDocument: composeControlDocument,
	})
	node := newMockNode()
	node.composeContainers = []ComposeContainer{
		{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"},
		{ID: "worker-id", Service: "worker", State: "running", Status: "Up 1 second"},
		{ID: "db-id", Service: "db", State: "running", Status: "Up 1 second"},
	}
	return repo, node, app, dep
}

func TestComposeStopStopsEveryProjectContainer(t *testing.T) {
	repo, node, app, _ := seededComposeProject(t)
	svc := newNodeService(t, repo, node)

	if _, err := svc.Stop(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, id := range []string{"web-id", "worker-id", "db-id"} {
		if !containsString(node.stopped, id) {
			t.Errorf("stopped = %v, want %q stopped", node.stopped, id)
		}
	}
	if node.stopCalls != len(node.composeContainers) {
		t.Errorf("stop calls = %d, want one per project container", node.stopCalls)
	}
	if node.removeCalls != 0 {
		t.Errorf("removed = %v, want a stop to keep every container", node.removed)
	}
	if len(node.removedVolumes) != 0 {
		t.Errorf("removed volumes = %v, want a stop to keep every volume", node.removedVolumes)
	}
}

func TestComposeStartStartsEveryProjectContainerInDependencyOrder(t *testing.T) {
	repo, node, app, _ := seededComposeProject(t)
	svc := newNodeService(t, repo, node)

	if _, err := svc.Start(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	want := []string{"db-id", "worker-id", "web-id"}
	if len(node.started) != len(want) {
		t.Fatalf("started = %v, want %v", node.started, want)
	}
	for index, id := range want {
		if node.started[index] != id {
			t.Fatalf("started = %v, want %v (dependencies first)", node.started, want)
		}
	}
}

func TestComposeStartWithoutDocumentKeepsListingOrder(t *testing.T) {
	repo, node, app, dep := seededComposeProject(t)
	dep.ComposeDocument = ""
	if _, err := repo.UpdateDeployment(context.Background(), dep); err != nil {
		t.Fatalf("update deployment: %v", err)
	}
	svc := newNodeService(t, repo, node)

	if _, err := svc.Start(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	want := []string{"web-id", "worker-id", "db-id"}
	if len(node.started) != len(want) {
		t.Fatalf("started = %v, want %v", node.started, want)
	}
	for index, id := range want {
		if node.started[index] != id {
			t.Fatalf("started = %v, want %v", node.started, want)
		}
	}
}

func TestComposeStopThenStartRevivesEveryProjectContainer(t *testing.T) {
	repo, node, app, _ := seededComposeProject(t)
	svc := newNodeService(t, repo, node)

	if _, err := svc.Stop(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, container := range node.composeContainers {
		if container.State != "exited" {
			t.Fatalf("container %q = %s, want the whole project stopped", container.Service, container.State)
		}
	}
	if _, err := svc.Start(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("start after stop: %v", err)
	}
	for _, container := range node.composeContainers {
		if container.State != "running" {
			t.Errorf("container %q = %s, want every service back", container.Service, container.State)
		}
	}
	want := []string{"db-id", "worker-id", "web-id"}
	if len(node.started) != len(want) {
		t.Fatalf("started = %v, want %v", node.started, want)
	}
	for index, id := range want {
		if node.started[index] != id {
			t.Fatalf("started = %v, want %v", node.started, want)
		}
	}
}

func TestComposeStopOnAStoppedProjectSucceeds(t *testing.T) {
	repo, node, app, _ := seededComposeProject(t)
	for index := range node.composeContainers {
		node.composeContainers[index].State = "exited"
	}
	svc := newNodeService(t, repo, node)

	stopped, err := svc.Stop(context.Background(), app.UserID, app.ID)
	if err != nil {
		t.Fatalf("stop of an already stopped project: %v", err)
	}
	if stopped.ID == uuid.Nil {
		t.Error("stop returned no deployment")
	}
	if node.removeCalls != 0 {
		t.Errorf("removed = %v, want a stop to keep every container", node.removed)
	}
}

func TestComposeStopWithoutProjectContainersIsNotFound(t *testing.T) {
	repo, node, app, _ := seededComposeProject(t)
	node.composeContainers = nil
	svc := newNodeService(t, repo, node)

	if _, err := svc.Stop(context.Background(), app.UserID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a project with no container", err)
	}
	if node.stopCalls != 0 {
		t.Errorf("stopped = %v, want no container touched", node.stopped)
	}
}

func TestComposeStopSurfacesAListingFailure(t *testing.T) {
	repo, node, app, _ := seededComposeProject(t)
	node.composePsErr = ErrAgentUnavailable
	svc := newNodeService(t, repo, node)

	if _, err := svc.Stop(context.Background(), app.UserID, app.ID); err == nil {
		t.Fatal("stop succeeded although the project containers could not be listed")
	}
	if node.stopCalls != 0 {
		t.Errorf("stopped = %v, want no container touched without a listing", node.stopped)
	}
}

func TestSingleContainerStopAndStartKeepTheRecordedContainer(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: node.containerID})
	svc := newNodeService(t, repo, node)

	if _, err := svc.Stop(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := svc.Start(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(node.stopped) != 1 || node.stopped[0] != node.containerID {
		t.Errorf("stopped = %v, want only the recorded container", node.stopped)
	}
	if len(node.started) != 1 || node.started[0] != node.containerID {
		t.Errorf("started = %v, want only the recorded container", node.started)
	}
	if node.composePsCalls != 0 {
		t.Errorf("compose ps calls = %d, want 0 for a single-container app", node.composePsCalls)
	}
}
