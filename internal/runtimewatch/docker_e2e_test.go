package runtimewatch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/services"
)

// TestRuntimeDetectorRealDockerEvidence proves the detector on real Docker
// evidence: a running replica becomes unavailable after a stop and healthy
// again after a restart. It is an opt-in probe because it needs a Docker
// engine and the alpine image.
func TestRuntimeDetectorRealDockerEvidence(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 for the Docker runtime failure/recovery probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := agent.NewDockerClient("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Version(ctx); err != nil {
		t.Skipf("Docker is unavailable: %v", err)
	}
	image := "alpine:3"
	if _, err := client.PullImage(ctx, image, "", ""); err != nil {
		t.Fatalf("pull %s: %v", image, err)
	}
	appID := uuid.New()
	project := services.ProjectName(appID)
	name := fmt.Sprintf("gotham-g85-probe-%d", time.Now().UnixNano())
	run := exec.CommandContext(ctx, "docker", "run", "-d", "--name", name,
		"--label", "com.docker.compose.project="+project,
		"--label", "com.docker.compose.service=web",
		"--label", "com.docker.compose.container-number=1",
		image, "sleep", "120")
	if output, runErr := run.CombinedOutput(); runErr != nil {
		t.Fatalf("start probe: %v %s", runErr, output)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", name).Run()
	})

	target := Target{ResourceType: ResourceApplication, ResourceID: appID, Compose: true}
	evidence := func() []ContainerEvidence {
		t.Helper()
		list, listErr := client.ListContainers(ctx, true, map[string]string{"com.docker.compose.project": project}, false)
		if listErr != nil {
			t.Fatalf("list probe: %v", listErr)
		}
		out := make([]ContainerEvidence, 0, len(list))
		for _, c := range list {
			details, inspectErr := client.InspectContainer(ctx, c.GetId())
			if inspectErr != nil {
				t.Fatalf("inspect probe: %v", inspectErr)
			}
			out = append(out, ContainerEvidence{
				ID:             c.GetId(),
				State:          details.State,
				Health:         details.Health,
				RestartCount:   details.RestartCount,
				OOMKilled:      details.OOMKilled,
				ExitCode:       details.ExitCode,
				RestartPolicy:  details.RestartPolicy,
				ComposeProject: c.GetLabels()["com.docker.compose.project"],
				ComposeService: c.GetLabels()["com.docker.compose.service"],
				Replica:        c.GetLabels()["com.docker.compose.container-number"],
				Managed:        true,
			})
		}
		return out
	}

	evaluate := func(now time.Time, previous ReplicaState) ReplicaState {
		t.Helper()
		replicas := associatedReplicas(target, evidence())
		if len(replicas) != 1 {
			t.Fatalf("replicas=%d, want 1", len(replicas))
		}
		return evaluateReplica(replicas[0], previous, now, now, "")
	}

	now := time.Now().UTC()
	running := evaluate(now, ReplicaState{})
	if running.State != "running" {
		t.Fatalf("running probe state=%q", running.State)
	}
	if running.Health != "unknown" || running.Reason != "no_healthcheck" {
		t.Fatalf("no-healthcheck probe=%+v", running)
	}

	if output, stopErr := exec.CommandContext(ctx, "docker", "stop", "--time", "0", name).CombinedOutput(); stopErr != nil {
		t.Fatalf("stop probe: %v %s", stopErr, output)
	}
	stopped := evaluate(time.Now().UTC().Add(time.Second), running)
	if stopped.State != "stopped" || stopped.Reason != "stopped" {
		t.Fatalf("stopped probe=%+v", stopped)
	}

	if output, startErr := exec.CommandContext(ctx, "docker", "start", name).CombinedOutput(); startErr != nil {
		t.Fatalf("start probe: %v %s", startErr, output)
	}
	recovered := evaluate(time.Now().UTC().Add(2*time.Second), stopped)
	if recovered.State != "running" {
		t.Fatalf("recovered probe=%+v", recovered)
	}
	if recovered.ContainerID != running.ContainerID {
		t.Fatal("restart changed the container id unexpectedly")
	}
}
