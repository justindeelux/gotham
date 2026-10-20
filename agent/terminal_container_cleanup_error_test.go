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

// TestDockerExecRealPreExistingSameUIDNonDumpable is round-5 finding 2: a
// same-uid non-dumpable process started BEFORE the terminal (a worker that
// dropped privileges, like nginx/php-fpm) is not attributable and must not fail
// the close. The terminal's own job is still killed.
func TestDockerExecRealPreExistingSameUIDNonDumpable(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainerImage(t, client, "debian:bookworm-slim")
	rootCtx, rootCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer rootCancel()
	rootSession, err := client.Exec(rootCtx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec(root): %v", err)
	}
	defer func() { _ = rootSession.Close() }()
	// Drop to nobody without exec: the process becomes non-dumpable while its
	// owner is the same uid as the later nobody terminal.
	preExisting := "perl -e '$<=65534; $>=65534; $)=65534; q{GOTHAM-PRE-EXISTING}; sleep 60' &\n"
	if _, err := rootSession.Write([]byte(preExisting)); err != nil {
		t.Fatalf("write pre-existing worker: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	if leaks := cmdlineLeaks(t, client, containerID, "GOTHAM-PRE-EXISTING"); leaks == "" {
		t.Fatal("the pre-existing non-dumpable worker did not start")
	}

	nobodyCtx, nobodyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer nobodyCancel()
	nobodySession, err := client.Exec(nobodyCtx, containerID, ExecOptions{User: "nobody", Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec(nobody): %v", err)
	}
	if _, err := nobodySession.Write([]byte("sleep 7803 &\n")); err != nil {
		t.Fatalf("write nobody job: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := nobodySession.Close(); err != nil {
		t.Fatalf("nobody Close = %v, want success (pre-existing worker is not ours)", err)
	}
	if !containerRunning(t, client, containerID) {
		t.Fatal("close stopped the application container")
	}
	waitNoCmdline(t, client, containerID, "sleep 7803")
	if leaks := cmdlineLeaks(t, client, containerID, "GOTHAM-PRE-EXISTING"); leaks == "" {
		t.Fatal("the pre-existing same-uid non-dumpable worker was killed")
	}
}

// TestDockerExecRealInTreeNonDumpableDescendant covers the positive side: a
// non-dumpable process STARTED FROM the terminal is in the tree/session, so it
// is attributed through readable /proc/<pid>/stat and killed even though its
// environ cannot be read.
func TestDockerExecRealInTreeNonDumpableDescendant(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainerImage(t, client, "debian:bookworm-slim")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	script := "arch=$(uname -m); if [ \"$arch\" = \"x86_64\" ]; then n=157; else n=167; fi; " +
		"perl -e \"syscall($n,4,0,0,0,0); q{GOTHAM-IN-TREE}; sleep 60\" &\n"
	if _, err := session.Write([]byte(script)); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	if leaks := cmdlineLeaks(t, client, containerID, "GOTHAM-IN-TREE"); leaks == "" {
		t.Fatal("the in-tree non-dumpable descendant did not start")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close = %v, want success after killing the in-tree descendant", err)
	}
	waitNoCmdline(t, client, containerID, "GOTHAM-IN-TREE")
	if !containerRunning(t, client, containerID) {
		t.Fatal("close stopped the application container")
	}
}

// TestDockerExecRealNonRootCloseWithRootProcess is the round-4/5 false-positive
// regression: a non-root (name or numeric uid) terminal sharing the container
// with a root-owned process must Close successfully; foreign-uid unreadable
// environ is not our marker space and must not fail the cleanup.
func TestDockerExecRealNonRootCloseWithRootProcess(t *testing.T) {
	client := e2eContainerClient(t)
	cases := []struct {
		name  string
		image string
		user  string
		job   string
	}{
		{"alpine-nobody", "alpine:3", "nobody", "sleep 7801"},
		{"busybox-nobody", "busybox:1.36", "nobody", "sleep 7802"},
		{"debian-nobody", "debian:bookworm-slim", "nobody", "sleep 7803"},
		{"alpine-uid-2000", "alpine:3", "2000", "sleep 7804"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, tc.image)
			rootCtx, rootCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer rootCancel()
			rootSession, err := client.Exec(rootCtx, containerID, ExecOptions{Cols: 100, Rows: 30})
			if err != nil {
				t.Fatalf("Exec(root): %v", err)
			}
			defer func() { _ = rootSession.Close() }()
			if err := writeInput(rootSession, "setsid sleep 7810 &\n"); err != nil {
				t.Fatalf("write root job: %v", err)
			}
			time.Sleep(400 * time.Millisecond)

			nonRootCtx, nonRootCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer nonRootCancel()
			nonRootSession, err := client.Exec(nonRootCtx, containerID, ExecOptions{
				User: tc.user, Cols: 100, Rows: 30,
			})
			if err != nil {
				t.Fatalf("Exec(%s): %v", tc.user, err)
			}
			if err := writeInput(nonRootSession, tc.job+" &\n"); err != nil {
				t.Fatalf("write non-root job: %v", err)
			}
			time.Sleep(400 * time.Millisecond)
			if err := nonRootSession.Close(); err != nil {
				t.Fatalf("non-root Close = %v, want success", err)
			}
			if !containerRunning(t, client, containerID) {
				t.Fatal("non-root close stopped the application container")
			}
			waitNoCmdline(t, client, containerID, tc.job)
			if leaks := cmdlineLeaks(t, client, containerID, "sleep 7810"); leaks == "" {
				t.Fatal("the unrelated root-owned process was killed")
			}
		})
	}
}

// writeInput is a small helper for a direct Docker exec session.
func writeInput(session ExecSession, input string) error {
	_, err := session.Write([]byte(input))
	return err
}

// TestDockerExecRealNonRootManyForeignProcesses is round-5 finding 1: a nobody
// helper must not spend the retry sleep on every foreign-uid process. 120 root
// sleeps are present; Close must finish well inside execKillTimeout (8 s).
func TestDockerExecRealNonRootManyForeignProcesses(t *testing.T) {
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
			if err := writeInput(rootSession, "i=0; while [ $i -lt 120 ]; do sleep 30 & i=$((i+1)); done\n"); err != nil {
				t.Fatalf("write root jobs: %v", err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if strings.Count(cmdlineLeaks(t, client, containerID, "sleep 30"), "\n") >= 100 {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			nobodyCtx, nobodyCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer nobodyCancel()
			nobodySession, err := client.Exec(nobodyCtx, containerID, ExecOptions{User: "nobody", Cols: 100, Rows: 30})
			if err != nil {
				t.Fatalf("Exec(nobody): %v", err)
			}
			if err := writeInput(nobodySession, "sleep 7820 &\n"); err != nil {
				t.Fatalf("write nobody job: %v", err)
			}
			time.Sleep(300 * time.Millisecond)
			start := time.Now()
			closeErr := nobodySession.Close()
			elapsed := time.Since(start)
			t.Logf("nobody Close with 120 root processes: %s", elapsed)
			if closeErr != nil {
				t.Fatalf("nobody Close = %v, want success", closeErr)
			}
			if elapsed > 4*time.Second {
				t.Fatalf("nobody Close took %s, want well under execKillTimeout (8s)", elapsed)
			}
			waitNoCmdline(t, client, containerID, "sleep 7820")
			if !containerRunning(t, client, containerID) {
				t.Fatal("close stopped the application container")
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
			if _, err := churn.Write([]byte("i=0; while [ $i -lt 150 ]; do /bin/true & i=$((i+1)); done\n")); err != nil {
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
