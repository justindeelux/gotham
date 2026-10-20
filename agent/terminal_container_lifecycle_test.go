package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// The lifecycle tests below are the required real-container regression for
// JUS-244 round 1: closing the hijacked attach connection alone does not end a
// Docker exec, so every path must end the shell and its jobs through the
// explicit kill helper. They use a unique throwaway alpine:3 container and
// never touch the shared gotham-dev-postgres / gotham-dev-redis containers.

func e2eContainerClient(t *testing.T) *DockerClient {
	t.Helper()
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live container exec lifecycle tests")
	}
	client, err := NewDockerClient("")
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

// e2eContainer starts a unique throwaway alpine:3 container.
func e2eContainer(t *testing.T, client *DockerClient) string {
	return e2eContainerImage(t, client, "alpine:3")
}

// e2eContainerImage starts a unique throwaway container of image and removes it
// at the end of the test.
func e2eContainerImage(t *testing.T, client *DockerClient, image string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := client.PullImage(ctx, image, "", ""); err != nil {
		t.Logf("pull %s: %v (using a local image if present)", image, err)
	}
	id, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:   image,
		Name:    "gotham-exec-lifecycle-" + uuid.NewString()[:8],
		Command: []string{"sleep", "600"},
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

// runContainerCommand runs one non-interactive command in the container and
// returns its output.
func runContainerCommand(t *testing.T, client *DockerClient, containerID, command string) string {
	t.Helper()
	return runContainerCommandArgs(t, client, containerID, command)
}

// runContainerCommandArgs is runContainerCommand with positional arguments for
// the script (kept out of the script text so a scan never matches itself).
func runContainerCommandArgs(t *testing.T, client *DockerClient, containerID, command string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := append([]string{"/bin/sh", "-c", command, "gotham-scan"}, args...)
	session, err := client.Exec(ctx, containerID, ExecOptions{Cmd: cmd})
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

// markerScanScript lists pids carrying the terminal's unique environment
// marker. It is a scan from outside the terminal exec (a fresh Exec), using
// /proc and tr/grep rather than the kill helper.
const markerScanScript = `exec 2>/dev/null
for d in /proc/[0-9]*; do
  [ -r "$d/environ" ] || continue
  if tr '\0' '\n' < "$d/environ" 2>/dev/null | grep -qx "GOTHAM_TERMINAL_ID=$1"; then
    echo "$d"
  fi
done`

// cmdlineScanScript lists pids whose command line contains $1. The scan shell
// is skipped by its gotham-scan argv[0].
const cmdlineScanScript = `exec 2>/dev/null
for d in /proc/[0-9]*; do
  [ -r "$d/cmdline" ] || continue
  cmd=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null)
  case "$cmd" in
    *gotham-scan*) ;;
    *"$1"*) echo "$d $cmd" ;;
  esac
done`

// terminalMarkerLeaks returns the /proc entries still carrying marker.
func terminalMarkerLeaks(t *testing.T, client *DockerClient, containerID, marker string) string {
	t.Helper()
	return strings.TrimSpace(runContainerCommandArgs(t, client, containerID, markerScanScript, marker))
}

// cmdlineLeaks returns the /proc entries whose command line contains needle.
func cmdlineLeaks(t *testing.T, client *DockerClient, containerID, needle string) string {
	t.Helper()
	return strings.TrimSpace(runContainerCommandArgs(t, client, containerID, cmdlineScanScript, needle))
}

// waitNoMarker polls until no process carries the terminal marker.
func waitNoMarker(t *testing.T, client *DockerClient, containerID, marker string) {
	t.Helper()
	const timeout = 10 * time.Second
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = terminalMarkerLeaks(t, client, containerID, marker)
		if last == "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("terminal marker %s still present in:\n%s", marker, last)
}

// waitNoCmdline polls until no process's command line contains needle.
func waitNoCmdline(t *testing.T, client *DockerClient, containerID, needle string) {
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

// containerRunning reports the container's Docker state.
func containerRunning(t *testing.T, client *DockerClient, containerID string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := client.inspectContainer(ctx, containerID)
	if err != nil {
		t.Fatalf("inspect container: %v", err)
	}
	return info.State.Running
}

// pidGone reports whether the pid no longer exists inside the container.
func pidGone(t *testing.T, client *DockerClient, containerID string, pid int) bool {
	t.Helper()
	output := runContainerCommand(t, client, containerID,
		fmt.Sprintf("if kill -0 %d 2>/dev/null; then echo alive; else echo gone; fi", pid))
	return strings.Contains(output, "gone")
}

// TestDockerExecRealCloseKillsShellAndJob is the finding-1 regression: close a
// terminal while a foreground job runs and prove neither the shell nor the job
// survives, while the application container keeps running.
func TestDockerExecRealCloseKillsShellAndJob(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	execSession, ok := session.(*dockerExecSession)
	if !ok {
		t.Fatalf("session type = %T, want *dockerExecSession", session)
	}
	shellPID := execSession.pid
	if _, err := session.Write([]byte("sleep 777\n")); err != nil {
		t.Fatalf("write foreground job: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	waitNoCmdline(t, client, containerID, "sleep 777")
	if !pidGone(t, client, containerID, shellPID) {
		t.Fatalf("terminal shell pid %d survived Close", shellPID)
	}
	if !containerRunning(t, client, containerID) {
		t.Fatal("closing the exec stopped the application container")
	}
}

// TestDockerExecRealTransportLossKillsShell proves a cancelled terminal stream
// (transport loss / revocation path) still runs the explicit cleanup through
// the real terminal server.
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

// TestDockerExecRealCtrlCResizeAndClose covers the interactive contract on a
// real container: Ctrl-C interrupts the foreground job and leaves the shell
// usable, resize is observed by stty, and closing the terminal removes the
// running job without stopping the container.
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

// TestDockerExecRealOpenCloseCyclesLeakNothing opens and closes several
// terminals, each running a job, and proves no shell or job accumulates.
func TestDockerExecRealOpenCloseCyclesLeakNothing(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)

	shellPIDs := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 80, Rows: 24})
		if err != nil {
			cancel()
			t.Fatalf("Exec %d: %v", i, err)
		}
		execSession, ok := session.(*dockerExecSession)
		if !ok {
			cancel()
			t.Fatalf("session type = %T", session)
		}
		shellPIDs = append(shellPIDs, execSession.pid)
		job := fmt.Sprintf("sleep 78%d\n", i)
		if _, err := session.Write([]byte(job)); err != nil {
			cancel()
			t.Fatalf("write job %d: %v", i, err)
		}
		time.Sleep(150 * time.Millisecond)
		if err := session.Close(); err != nil {
			cancel()
			t.Fatalf("Close %d: %v", i, err)
		}
		cancel()
	}

	for i := 0; i < 3; i++ {
		waitNoCmdline(t, client, containerID, fmt.Sprintf("sleep 78%d", i))
	}
	for _, pid := range shellPIDs {
		if !pidGone(t, client, containerID, pid) {
			t.Fatalf("terminal shell pid %d survived", pid)
		}
	}
	if !containerRunning(t, client, containerID) {
		t.Fatal("open/close cycles stopped the application container")
	}
}
