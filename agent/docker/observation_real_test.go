package docker

import (
	"context"
	"os"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestInspectContainerRealEvidence is the live throwaway-container probe: it
// verifies the fields monitoring reports (engine data root, state, restart/OOM
// evidence and lifecycle timestamps) come from a real engine response. It runs
// only with GOTHAM_E2E=1 so the default suite never needs a Docker daemon.
func TestInspectContainerRealEvidence(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live container observation probe")
	}
	client := e2eContainerClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root, err := client.DockerRootDir(ctx)
	if err != nil || root == "" {
		t.Fatalf("DockerRootDir = %q, %v; want the engine data root", root, err)
	}

	id, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:         "alpine:3",
		Name:          "gotham-observation-probe",
		Command:       []string{"sleep", "600"},
		RestartPolicy: "always",
		Labels:        map[string]string{"gotham.managed": "true"},
	})
	if err != nil {
		t.Skipf("run throwaway container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Remove(cleanupCtx, id)
	})

	details, err := client.InspectContainer(ctx, id)
	if err != nil {
		t.Fatalf("InspectContainer: %v", err)
	}
	if details.State != "running" {
		t.Errorf("state = %q; want running", details.State)
	}
	if details.RestartCount != 0 || details.OOMKilled {
		t.Errorf("restart/oom = %d/%v; want fresh evidence", details.RestartCount, details.OOMKilled)
	}
	if details.CreatedAt.IsZero() || details.StartedAt.IsZero() {
		t.Errorf("lifecycle = %+v; want created/started timestamps", details)
	}

	// A memory-limited container that allocates past its limit gives explicit
	// OOMKilled and restart evidence rather than a zero placeholder.
	oomID, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:         "alpine:3",
		Name:          "gotham-observation-oom",
		Command:       []string{"sh", "-c", `x=$(head -c 20000000 /dev/zero | tr "\0" a); sleep 5`},
		RestartPolicy: "on-failure",
		MemoryBytes:   8 << 20,
		Labels:        map[string]string{"gotham.managed": "true"},
	})
	if err != nil {
		t.Skipf("run OOM probe container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Remove(cleanupCtx, oomID)
	})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		oom, err := client.InspectContainer(ctx, oomID)
		if err != nil {
			t.Fatalf("InspectContainer(OOM): %v", err)
		}
		if oom.OOMKilled && oom.RestartCount > 0 && !oom.FinishedAt.IsZero() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Error("the OOM probe never reported OOMKilled with a restart count")
}
