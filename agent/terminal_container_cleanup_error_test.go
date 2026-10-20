package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestDockerExecRealMarkerScanUnavailableWithoutTools is the round-3 finding-1
// repro: a minimal image without tr/grep cannot inspect markers. Close must
// report the incomplete cleanup instead of returning success while an orphaned
// loop survives.
func TestDockerExecRealMarkerScanUnavailableWithoutTools(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// Remove the marker tools, then leave an orphan that ignores HUP and exit
	// the shell so Close takes the marker-only path.
	if _, err := session.Write([]byte("rm -f /usr/bin/tr /bin/tr /bin/grep /usr/bin/grep; (trap '' HUP; while :; do :; done) & exit 0\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	closeErr := session.Close()
	if closeErr == nil || !strings.Contains(closeErr.Error(), "marker-scan-unavailable") {
		t.Fatalf("Close = %v, want marker-scan-unavailable", closeErr)
	}
}

// TestDockerExecRealMarkerScanUnreadableEnviron is the other finding-1 repro: a
// non-root helper cannot read a root-owned process's environ. The helper must
// report that it cannot inspect markers instead of returning success.
func TestDockerExecRealMarkerScanUnreadableEnviron(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	marker := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{
		Cols: 100, Rows: 30,
		Env: []string{"GOTHAM_TERMINAL_ID=" + marker},
	})
	if err != nil {
		t.Fatalf("Exec(root): %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Write([]byte("setsid sleep 7702 &\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	output, err := client.killExecMarker(context.Background(), containerID, "nobody", "", marker)
	if err == nil || !strings.Contains(err.Error()+output, "marker-scan-unavailable") {
		t.Fatalf("killExecMarker = (%q, %v), want marker-scan-unavailable", output, err)
	}
}

// TestDockerClientExecRealHelperOutputIsClean is the round-3 finding-4 regression:
// processes exiting during the scan must not print redirection errors into the
// helper output.
func TestDockerClientExecRealHelperOutputIsClean(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	churn, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	defer func() { _ = churn.Close() }()
	if _, err := churn.Write([]byte("i=0; while [ $i -lt 300 ]; do /bin/true & i=$((i+1)); done\n")); err != nil {
		t.Fatalf("write churn: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	output, err := client.killExecMarker(context.Background(), containerID, "", "", uuid.NewString())
	if err != nil {
		t.Fatalf("killExecMarker = (%q, %v)", output, err)
	}
	if strings.Contains(output, "can't open") || strings.Contains(output, "No such process") {
		t.Fatalf("helper output is noisy: %q", output)
	}
}
