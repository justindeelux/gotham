package deploy

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/servers"
)

// TestLogSnapshotDockerAgentReal uses only a unique stopped throwaway container and loopback agent.
func TestLogSnapshotDockerAgentReal(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 for the disposable Docker/agent proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	user := uuid.New()
	app := testApplication(user)
	name := "gotham-snapshot-" + uuid.NewString()
	output, err := exec.CommandContext(ctx, "docker", "run", "--name", name, "--label", labelAppID+"="+app.ID.String(), "--memory", "32m", "--pids-limit", "32", "--cpus", "0.5", "alpine:3", "sh", "-c", "printf 'first\\nsecond\\nthird\\n'").CombinedOutput()
	t.Cleanup(func() {
		cleanup, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
	})
	if err != nil {
		t.Fatalf("throwaway container: %s %v", output, err)
	}
	docker, err := agent.NewDockerClient("")
	if err != nil {
		t.Fatal(err)
	}
	server, err := agent.NewServer("127.0.0.1:0", nil, agent.NewDockerServer(docker, discardLogger()), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	done := make(chan error, 1)
	go func() { done <- server.Serve(agentCtx) }()
	t.Cleanup(func() {
		stopAgent()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("agent did not stop")
		}
	})
	svc := NewService(Config{Repository: &fakeRepository{app: app}, Secret: testSecretKey, Logger: discardLogger(), Dial: func(ctx context.Context, _ uuid.UUID) (Node, error) {
		client, err := servers.DialDockerClient(ctx, server.Addr().String(), nil)
		if err != nil {
			return nil, err
		}
		return newAgentNode(client), nil
	}})
	t.Cleanup(func() { _ = svc.Close() })
	out, err := svc.GetApplicationLogs(ctx, user, app.ID, LogOptions{Tail: 10})
	if err != nil || !out.Complete || len(out.Containers) != 1 || out.Containers[0].Log != "first\nsecond\nthird\n" || out.Containers[0].State != "exited" || len(out.Containers[0].ID) != 64 {
		t.Fatalf("real snapshot=%+v %v", out, err)
	}
	bounded, err := svc.GetApplicationLogs(ctx, user, app.ID, LogOptions{Tail: 2, MaxBytes: 4})
	if err != nil || bounded.Complete || bounded.Containers[0].Log != "seco" {
		t.Fatalf("real cap=%+v %v", bounded, err)
	}
	empty, err := exec.CommandContext(ctx, "docker", "logs", name).Output()
	if err != nil || !strings.Contains(string(empty), "third") {
		t.Fatal("throwaway fixture lost logs")
	}
}
