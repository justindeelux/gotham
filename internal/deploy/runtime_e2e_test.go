package deploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestRuntimeComposeReal uses only a unique throwaway project and always removes it.
func TestRuntimeComposeReal(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 for the Docker/Compose runtime proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	docker, err := agent.NewDockerClient("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = docker.Version(ctx); err != nil {
		t.Fatal(err)
	}
	if err = exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
		t.Fatal(err)
	}
	if _, err = docker.PullImage(ctx, "alpine:3", "", ""); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	compose := agent.NewComposeServer(agent.ComposeServerConfig{Root: root})
	user := uuid.New()
	app := testApplication(user)
	app.SourceType = SourceCompose
	project := services.ProjectName(app.ID)
	document := `services:
  web:
    image: alpine:3
    command: ["sleep", "120"]
    deploy:
      replicas: 2
    healthcheck:
      test: ["CMD", "false"]
      interval: 1s
      timeout: 1s
      retries: 1
    restart: "no"
    pids_limit: 32
    mem_limit: 32m
    cpus: "0.5"
  worker:
    image: alpine:3
    command: ["sleep", "120"]
    restart: "no"
    pids_limit: 32
    mem_limit: 32m
    cpus: "0.5"
`
	t.Cleanup(func() { composeDown(t, compose, project, document) })
	up := func() {
		t.Helper()
		if _, upErr := compose.ComposeUp(ctx, &agentv1.ComposeUpRequest{ProjectName: project, ComposeYaml: []byte(document), Unconfined: true}); upErr != nil {
			t.Fatal(upErr)
		}
	}
	up()
	node := &composeRealNode{mockNode: newMockNode(), compose: compose, docker: docker}
	repo := &fakeRepository{app: app, deployments: []Deployment{{ID: uuid.New(), ApplicationID: app.ID, State: StateRunning}}}
	svc := newNodeService(t, repo, node)
	first, err := svc.GetApplicationRuntime(ctx, user, app.ID)
	if err != nil || len(first.Containers) != 3 {
		t.Fatalf("runtime=%+v %v", first, err)
	}
	up()
	reused, err := svc.GetApplicationRuntime(ctx, user, app.ID)
	if err != nil || len(reused.Containers) != 3 {
		t.Fatalf("runtime=%+v %v", reused, err)
	}
	workerID := ""
	for i, c := range reused.Containers {
		if c.ID != first.Containers[i].ID || c.DeploymentID != nil {
			t.Fatalf("unchanged up invented new release: %+v", c)
		}
		if c.Service == "worker" {
			workerID = c.ID
			if c.Health != "unknown" {
				t.Fatalf("no healthcheck asserted readiness: %+v", c)
			}
		}
	}
	if workerID == "" {
		t.Fatal("worker missing")
	}
	if output, stopErr := exec.CommandContext(ctx, "docker", "stop", "--time", "0", workerID).CombinedOutput(); stopErr != nil {
		t.Fatalf("stop fixture: %v %s", stopErr, output)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		out, readErr := svc.GetApplicationRuntime(ctx, user, app.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		unhealthy, stopped := 0, 0
		for _, c := range out.Containers {
			if c.DeploymentID != nil {
				t.Fatal("Compose project membership associated exact deployment")
			}
			if c.Service == "web" && c.Health == "unhealthy" {
				unhealthy++
			}
			if c.Service == "worker" && c.State == "exited" {
				stopped++
			}
		}
		if unhealthy == 2 && stopped == 1 {
			t.Logf("observed 2 unhealthy replicas and 1 stopped worker, with reused IDs and unknown exact deployment association")
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("partial outage not reported: %+v", out)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
