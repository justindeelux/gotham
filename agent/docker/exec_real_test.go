package docker

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

// The real-container exec tests below are the required regression for JUS-244:
// closing the hijacked attach connection alone does not end a Docker exec, so
// every path must end the shell and its jobs through the explicit kill helper.
// They use a unique throwaway container and never touch the shared dev
// containers.

var killTableCases = []struct {
	name   string
	input  string
	needle string
}{
	{"foreground", "sleep 7101\n", ""},
	{"trap-ignores-hup-term-int", "trap '' HUP TERM INT; sleep 7102\n", ""},
	{"nohup-background", "nohup sleep 7103 &\n", ""},
	{"plain-background", "sleep 7104 &\n", ""},
	{"setsid-background", "(setsid sleep 7105 &); sleep 7106\n", ""},
	{"nested-background", "( (sleep 7107 &) & ); sleep 7108\n", ""},
	// A newline in the shell's comm used to truncate /proc/<pid>/stat on the
	// first line; the child scrubs its environment so only the parent chain can
	// find it.
	{"comm-newline-env-scrubbed", "printf 'a\nb' > /proc/self/comm; env -i sleep 7701 &\n", "sleep 7701"},
	{"shell-exit-before-close", "exit 0\n", ""},
}

// e2eContainerClient returns a Docker client, skipping unless GOTHAM_E2E=1.
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

// cmdlineLeaks returns the /proc entries whose command line contains needle.
func cmdlineLeaks(t *testing.T, client *DockerClient, containerID, needle string) string {
	t.Helper()
	return strings.TrimSpace(runContainerCommandArgs(t, client, containerID, cmdlineScanScript, needle))
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

// terminalMarkerLeaks returns the /proc entries still carrying marker.
func terminalMarkerLeaks(t *testing.T, client *DockerClient, containerID, marker string) string {
	t.Helper()
	return strings.TrimSpace(runContainerCommandArgs(t, client, containerID, markerScanScript, marker))
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

// pidGone reports whether the pid no longer exists inside the container.
func pidGone(t *testing.T, client *DockerClient, containerID string, pid int) bool {
	t.Helper()
	output := runContainerCommand(t, client, containerID,
		fmt.Sprintf("if kill -0 %d 2>/dev/null; then echo alive; else echo gone; fi", pid))
	return strings.Contains(output, "gone")
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

func TestDockerExecRealKillTable(t *testing.T) {
	client := e2eContainerClient(t)
	for _, image := range []string{"alpine:3", "busybox:1.36", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			for _, testCase := range killTableCases {
				t.Run(testCase.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
					if err != nil {
						t.Fatalf("Exec: %v", err)
					}
					dockerSession, ok := session.(*dockerExecSession)
					if !ok {
						t.Fatalf("session type = %T", session)
					}
					if _, err := session.Write([]byte(testCase.input)); err != nil {
						t.Fatalf("write: %v", err)
					}
					time.Sleep(450 * time.Millisecond)
					if err := session.Close(); err != nil {
						t.Fatalf("Close: %v", err)
					}
					running, err := client.execRunning(context.Background(), dockerSession.id)
					if err != nil {
						t.Fatalf("inspect exec: %v", err)
					}
					if running {
						t.Fatal("Docker still reports the exec as Running after Close")
					}
					waitNoMarker(t, client, containerID, dockerSession.marker)
					if testCase.needle != "" {
						waitNoCmdline(t, client, containerID, testCase.needle)
					}
					if !containerRunning(t, client, containerID) {
						t.Fatal("closing the terminal stopped the application container")
					}
				})
			}
		})
	}
}

// TestDockerExecRealCleanupFailureIsSurfaced removes the helper's /bin/sh after
// the terminal is open. Close must return an error and must not pretend the exec
// is gone; the container is removed by cleanup.
func TestDockerExecRealCleanupFailureIsSurfaced(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := session.Write([]byte("chmod -x /bin/sh; sleep 7600\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	closeErr := session.Close()
	if closeErr == nil || (!strings.Contains(closeErr.Error(), "cleanup incomplete") && !strings.Contains(closeErr.Error(), "still running")) {
		t.Fatalf("Close = %v, want a surfaced cleanup failure", closeErr)
	}
	running, inspectErr := client.execRunning(context.Background(), session.(*dockerExecSession).id)
	if inspectErr != nil {
		t.Fatalf("inspect exec: %v", inspectErr)
	}
	if !running {
		t.Fatal("cleanup failure was reported but the exec is already gone")
	}
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
			t.Logf("elapsed nobody Close with 120 root processes: %s", elapsed)
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
