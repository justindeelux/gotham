package terminal

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent/docker"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func e2eContainerClient(t *testing.T) *docker.DockerClient {
	t.Helper()
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live container exec lifecycle tests")
	}
	client, err := docker.NewDockerClient("")
	if err != nil {
		t.Skipf("docker client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	return client
}

func e2eContainer(t *testing.T, client *docker.DockerClient) string {
	return e2eContainerImage(t, client, "alpine:3")
}

func e2eContainerImage(t *testing.T, client *docker.DockerClient, image string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := client.PullImage(ctx, image, "", ""); err != nil {
		t.Logf("pull %s: %v (using a local image if present)", image, err)
	}
	id, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:       image,
		Name:        "gotham-exec-lifecycle-" + uuid.NewString()[:8],
		Command:     []string{"sleep", "600"},
		PidsLimit:   256,
		MemoryBytes: 256 << 20,
		NanoCpus:    1_000_000_000,
	})
	if err != nil {
		t.Skipf("run throwaway %s container: %v", image, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Remove(cleanupCtx, id)
	})
	return id
}

func runContainerCommandArgs(t *testing.T, client *docker.DockerClient, containerID, command string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := append([]string{"/bin/sh", "-c", command, "gotham-scan"}, args...)
	session, err := client.Exec(ctx, containerID, docker.ExecOptions{Cmd: cmd})
	if err != nil {
		t.Fatalf("exec scan: %v", err)
	}
	output, readErr := io.ReadAll(session)
	if readErr != nil {
		t.Logf("read scan: %v", readErr)
	}
	_ = session.Close()
	return string(output)
}

const cmdlineScanScript = `exec 2>/dev/null
for d in /proc/[0-9]*; do
  [ -r "$d/cmdline" ] || continue
  cmd=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null)
  case "$cmd" in
    *gotham-scan*) ;;
    *"$1"*) echo "$d $cmd" ;;
  esac
done`

func cmdlineLeaks(t *testing.T, client *docker.DockerClient, containerID, needle string) string {
	t.Helper()
	return strings.TrimSpace(runContainerCommandArgs(t, client, containerID, cmdlineScanScript, needle))
}

func waitNoCmdline(t *testing.T, client *docker.DockerClient, containerID, needle string) {
	t.Helper()
	const timeout = 10 * time.Second
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = cmdlineLeaks(t, client, containerID, needle)
		if last == "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("process %q still running inside the container:\n%s", needle, last)
}

func containerRunning(t *testing.T, client *docker.DockerClient, containerID string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	containers, err := client.ListContainers(ctx, true, nil, false)
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	for _, container := range containers {
		if container.GetId() == containerID {
			return container.GetState() == "running"
		}
	}
	return false
}

func TestDockerExecRealTransportLossKillsShell(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)

	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
	grpcClient := startTerminalServer(t, terminal)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := grpcClient.OpenTerminal(ctx)
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 778\n")}}); err != nil {
		t.Fatalf("send job: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()

	waitNoCmdline(t, client, containerID, "sleep 778")
	if !containerRunning(t, client, containerID) {
		t.Fatal("transport loss stopped the application container")
	}
}

func TestDockerExecRealCtrlCResizeAndClose(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)

	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
	grpcClient := startTerminalServer(t, terminal)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := grpcClient.OpenTerminal(ctx)
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "container", ContainerId: containerID, Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 1000\n")}}); err != nil {
		t.Fatalf("send sleep: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte{0x03}}}); err != nil {
		t.Fatalf("send Ctrl-C: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo after-ctrl-c\n")}}); err != nil {
		t.Fatalf("send after ctrl-c: %v", err)
	}
	waitForOutput(t, stream, "after-ctrl-c")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Resize{
		Resize: &agentv1.TerminalResize{Cols: 120, Rows: 40},
	}}); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("stty size\n")}}); err != nil {
		t.Fatalf("send stty: %v", err)
	}
	waitForOutput(t, stream, "40 120")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 779\n")}}); err != nil {
		t.Fatalf("send closing job: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close send: %v", err)
	}
	waitNoCmdline(t, client, containerID, "sleep 779")
	if !containerRunning(t, client, containerID) {
		t.Fatal("closing the terminal stopped the application container")
	}
}

func TestDockerExecRealTransportLossTable(t *testing.T) {
	client := e2eContainerClient(t)
	cases := []struct {
		name    string
		input   string
		needles []string
	}{
		{"trap", "trap '' HUP TERM INT; sleep 7301\n", []string{"sleep 7301"}},
		{"nohup", "nohup sleep 7302 &\n", []string{"sleep 7302"}},
		{"setsid", "(setsid sleep 7303 &); sleep 7304\n", []string{"sleep 7303", "sleep 7304"}},
		{"nested", "( (sleep 7305 &) & ); sleep 7306\n", []string{"sleep 7305", "sleep 7306"}},
	}
	for _, image := range []string{"alpine:3", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
			grpcClient := startTerminalServer(t, terminal)
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					stream, err := grpcClient.OpenTerminal(ctx)
					if err != nil {
						t.Fatalf("OpenTerminal: %v", err)
					}
					if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
						Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
					}}}); err != nil {
						t.Fatalf("send open: %v", err)
					}
					if _, err := stream.Recv(); err != nil {
						t.Fatalf("recv opened: %v", err)
					}
					if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte(testCase.input)}}); err != nil {
						t.Fatalf("send job: %v", err)
					}
					time.Sleep(450 * time.Millisecond)
					cancel()
					for _, needle := range testCase.needles {
						waitNoCmdline(t, client, containerID, needle)
					}
					if !containerRunning(t, client, containerID) {
						t.Fatal("transport loss stopped the application container")
					}
				})
			}
		})
	}
}

func TestDockerExecRealJobControl(t *testing.T) {
	client := e2eContainerClient(t)
	for _, image := range []string{"alpine:3", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
			grpcClient := startTerminalServer(t, terminal)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := grpcClient.OpenTerminal(ctx)
			if err != nil {
				t.Fatalf("OpenTerminal: %v", err)
			}
			if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
				Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
			}}}); err != nil {
				t.Fatalf("send open: %v", err)
			}
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("recv opened: %v", err)
			}
			send := func(data string) {
				t.Helper()
				if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte(data)}}); err != nil {
					t.Fatalf("send %q: %v", data, err)
				}
			}
			send("sleep 7400\n")
			time.Sleep(300 * time.Millisecond)
			send(string([]byte{0x1a}))
			time.Sleep(200 * time.Millisecond)
			send("jobs\n")
			waitForOutput(t, stream, "Stopped")
			send("bg\n")
			time.Sleep(200 * time.Millisecond)
			send("jobs\n")
			waitForOutput(t, stream, "Running")
			send("fg\n")
			time.Sleep(300 * time.Millisecond)
			send(string([]byte{0x03}))
			time.Sleep(300 * time.Millisecond)
			send("echo JOB-$((6*7))\n")
			waitForOutput(t, stream, "JOB-42")
			send("exit\n")
			_ = stream.CloseSend()
			waitNoCmdline(t, client, containerID, "sleep 7400")
			if !containerRunning(t, client, containerID) {
				t.Fatal("job control stopped the application container")
			}
		})
	}
}

func TestDockerExecRealConcurrentCleanupPaths(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)

	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
	grpcClient := startTerminalServer(t, terminal)
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := grpcClient.OpenTerminal(ctx)
		if err != nil {
			t.Fatalf("OpenTerminal: %v", err)
		}
		if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
			Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
		}}}); err != nil {
			t.Fatalf("send open: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("recv opened: %v", err)
		}
		if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 7500\n")}}); err != nil {
			t.Fatalf("send job: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		var wait sync.WaitGroup
		wait.Add(2)
		go func() { defer wait.Done(); _ = stream.CloseSend() }()
		go func() { defer wait.Done(); cancel() }()
		wait.Wait()
		waitNoCmdline(t, client, containerID, "sleep 7500")
	}
	if !containerRunning(t, client, containerID) {
		t.Fatal("concurrent close/cancel stopped the application container")
	}

	capped := NewTerminalServer(TerminalConfig{
		Logger:      discardLogger(),
		Containers:  client,
		MaxDuration: 1500 * time.Millisecond,
	})
	cappedClient := startTerminalServer(t, capped)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := cappedClient.OpenTerminal(ctx)
	if err != nil {
		t.Fatalf("OpenTerminal (cap): %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
	}}}); err != nil {
		t.Fatalf("send open (cap): %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened (cap): %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 7501\n")}}); err != nil {
		t.Fatalf("send job (cap): %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	waitNoCmdline(t, client, containerID, "sleep 7501")
	if !containerRunning(t, client, containerID) {
		t.Fatal("the lifetime cap stopped the application container")
	}
}
