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

// TestDockerExecRealMarkerScanUnreadableSameUID is the round-4 same-uid
// failure repro: a non-dumpable root process (perl prctl PR_SET_DUMPABLE=0) is
// owned by the helper's uid but its environ cannot be opened. The helper must
// report that it cannot inspect markers.
func TestDockerExecRealMarkerScanUnreadableSameUID(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainerImage(t, client, "debian:bookworm-slim")
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
	// prctl is syscall 157 on x86_64 and 167 on arm64. The process stays
	// root-owned and non-dumpable, so /proc/<pid>/environ opens fail even for
	// the same uid without CAP_SYS_PTRACE.
	script := "arch=$(uname -m); if [ \"$arch\" = \"x86_64\" ]; then n=157; else n=167; fi; " +
		"perl -e \"syscall($n,4,0,0,0,0); sleep 60\" &\n"
	if _, err := session.Write([]byte(script)); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(700 * time.Millisecond)

	output, err := client.killExecMarker(context.Background(), containerID, "", "", marker)
	if err == nil || !strings.Contains(err.Error()+output, "marker-scan-unavailable") {
		t.Fatalf("killExecMarker = (%q, %v), want marker-scan-unavailable", output, err)
	}
}

// TestDockerExecRealNonRootCloseWithRootProcess is the round-4 false-positive
// regression: a nobody terminal sharing the container with a root-owned
// process must Close successfully; foreign-uid unreadable environ is not our
// marker space and must not fail the cleanup.
func TestDockerExecRealNonRootCloseWithRootProcess(t *testing.T) {
	client := e2eContainerClient(t)
	for _, image := range []string{"alpine:3", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			rootCtx, rootCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer rootCancel()
			rootSession, err := client.Exec(rootCtx, containerID, ExecOptions{Cols: 100, Rows: 30})
			if err != nil {
				t.Fatalf("Exec(root): %v", err)
			}
			defer func() { _ = rootSession.Close() }()
			if _, err := rootSession.Write([]byte("setsid sleep 7801 &\n")); err != nil {
				t.Fatalf("write root job: %v", err)
			}
			time.Sleep(400 * time.Millisecond)

			nobodyCtx, nobodyCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer nobodyCancel()
			nobodySession, err := client.Exec(nobodyCtx, containerID, ExecOptions{
				User: "nobody", Cols: 100, Rows: 30,
			})
			if err != nil {
				t.Fatalf("Exec(nobody): %v", err)
			}
			if _, err := nobodySession.Write([]byte("sleep 7802 &\n")); err != nil {
				t.Fatalf("write nobody job: %v", err)
			}
			time.Sleep(400 * time.Millisecond)
			if err := nobodySession.Close(); err != nil {
				t.Fatalf("nobody Close = %v, want success", err)
			}
			if !containerRunning(t, client, containerID) {
				t.Fatal("non-root close stopped the application container")
			}
			waitNoCmdline(t, client, containerID, "sleep 7802")
			if leaks := cmdlineLeaks(t, client, containerID, "sleep 7801"); leaks == "" {
				t.Fatal("the unrelated root-owned process was killed")
			}
		})
	}
}

// TestDockerClientExecRealHelperOutputIsClean is the round-3 finding-4 regression:
// processes exiting during the scan must not print redirection errors into the
// helper output.
func TestDockerClientExecRealHelperOutputIsClean(t *testing.T) {
	client := e2eContainerClient(t)
	for _, image := range []string{"alpine:3", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
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
			for _, noise := range []string{"can't open", "cannot open", "No such process"} {
				if strings.Contains(output, noise) {
					t.Fatalf("helper output is noisy (%q): %q", noise, output)
				}
			}
		})
	}
}
