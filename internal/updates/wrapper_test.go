package updates

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// wrapperResult captures the outcome of one gotham-update.sh run.
type wrapperResult struct {
	exitCode int
	target   string
	backup   string
	status   string
	pending  string
	output   string
	// statusDir is the scratch root-owned status directory of the run.
	statusDir string
	// systemctlLog is the fake systemctl call log (mode rate-limit only).
	systemctlLog string
}

// health modes for the fake curl shim.
const (
	healthAlwaysOK    = "ok"             // the service is always healthy
	healthAlwaysFail  = "fail"           // the service is never healthy
	healthAfterBackup = "after-rollback" // healthy only once the old binary is back
)

// systemctl shim modes.
const (
	// systemctlRateLimit models systemd's start rate limit: a restart succeeds
	// once, then fails with "Start request repeated too quickly" until
	// reset-failed clears the failed state.
	systemctlRateLimit = "rate-limit"
)

// wrapperEnv describes one wrapper run.
type wrapperEnv struct {
	systemctlExit int
	health        string
	// systemctlMode selects a richer fake systemctl. Empty uses systemctlExit.
	systemctlMode string
	// flockShim selects a richer fake flock. Empty exits 0 immediately.
	flockShim string
	// setup runs after the default files are created, to plant symlinks etc.
	setup func(dir, target, statusPath string)
}

// flock shim modes.
const (
	// flockSwapWhileWaiting models the service user swapping the lock path
	// while root waits on it: the wrapper must refuse, not run unserialized.
	flockSwapWhileWaiting = "swap"
)

// runWrapper runs the deployed wrapper with fake systemctl/curl shims. It
// reproduces the reviewers' repros: a restart that exits nonzero must still
// restore the previous binary, and a pre-planted symlink must not redirect the
// root status write.
func runWrapper(t *testing.T, env wrapperEnv) wrapperResult {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gotham-update.sh targets POSIX")
	}

	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fakebin: %v", err)
	}
	systemctlLog := filepath.Join(dir, "systemctl.log")
	if env.systemctlMode == systemctlRateLimit {
		writeRateLimitShim(t, filepath.Join(fakeBin, "systemctl"), systemctlLog)
	} else {
		writeSystemctlShim(t, filepath.Join(fakeBin, "systemctl"), env.systemctlExit)
	}
	writeCurlShim(t, filepath.Join(fakeBin, "curl"), env.health)
	switch env.flockShim {
	case flockSwapWhileWaiting:
		writeFlockSwapShim(t, filepath.Join(fakeBin, "flock"))
	default:
		writeShim(t, filepath.Join(fakeBin, "flock"), 0)
	}

	target := filepath.Join(dir, "bin", "gotham")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	backup := target + OldSuffix
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	statusDir := filepath.Join(dir, "statusdir")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatalf("mkdir statusdir: %v", err)
	}
	status := filepath.Join(statusDir, "update.status")
	pending := filepath.Join(dir, "update.pending")
	if err := os.WriteFile(pending, []byte("result=staged\nversion=v1.2.0\n"), 0o644); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	lock := filepath.Join(dir, "update.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	conf := filepath.Join(dir, "updater.conf")
	confBody := strings.Join([]string{
		"GOTHAM_BINARY=" + target,
		"GOTHAM_SERVICE=fake",
		"GOTHAM_HEALTH=http://127.0.0.1:1/healthz",
		"GOTHAM_TIMEOUT=1",
		"GOTHAM_STATUS=" + status,
		"GOTHAM_PENDING=" + pending,
		"GOTHAM_LOCK=" + lock,
		"GOTHAM_GRACE=0",
	}, "\n") + "\n"
	if err := os.WriteFile(conf, []byte(confBody), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	if env.setup != nil {
		env.setup(dir, target, status)
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", script)
	cmd.Env = append(sanitizedEnv(),
		"GOTHAM_UPDATER_CONF="+conf,
		"WRAPPER_TEST_TARGET="+target,
		"WRAPPER_TEST_LOCK="+lock,
		"WRAPPER_TEST_STATUS_DIR="+statusDir,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, runErr := cmd.CombinedOutput()

	result := wrapperResult{output: string(output), statusDir: statusDir}
	if runErr != nil {
		exitErr, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("run wrapper: %v", runErr)
		}
		result.exitCode = exitErr.ExitCode()
	}
	result.target = readRegularOrEmpty(target)
	result.backup = readRegularOrEmpty(backup)
	result.status = readRegularOrEmpty(status)
	result.pending = readRegularOrEmpty(pending)
	result.systemctlLog = readRegularOrEmpty(systemctlLog)
	return result
}

// readRegularOrEmpty returns a file's contents only when it is a regular file.
// The tests plant FIFOs/symlinks at the wrapper's paths, and os.ReadFile on one
// would block the harness forever; anything non-regular (or missing) yields "".
// The open uses O_NONBLOCK so even a swap after the Lstat cannot block.
func readRegularOrEmpty(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return ""
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return ""
	}
	return string(data)
}

// sanitizedEnv returns os.Environ() without sudo's variables. The harness
// models the non-sudo GOTHAM_UPDATER_CONF seam, so how `go test` was invoked
// (plain or via sudo) must not change what the wrapper touches.
func sanitizedEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "SUDO_USER", "SUDO_UID", "SUDO_GID":
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// realInstallPresent reports whether this host has a real Gotham control plane.
// Safety tests skip rather than risk acting on it.
func realInstallPresent() bool {
	for _, path := range []string{"/etc/gotham/updater.conf", "/var/lib/gotham", "/var/lib/gotham-updater"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// TestSanitizedEnvStripsSudo pins the harness itself: CI has no SUDO_*
// variables, so without this a revert of sanitizedEnv would still pass CI.
func TestSanitizedEnvStripsSudo(t *testing.T) {
	t.Setenv("SUDO_USER", "root")
	t.Setenv("SUDO_UID", "0")
	t.Setenv("SUDO_GID", "0")
	for _, entry := range sanitizedEnv() {
		for _, prefix := range []string{"SUDO_USER=", "SUDO_UID=", "SUDO_GID="} {
			if strings.HasPrefix(entry, prefix) {
				t.Fatalf("sanitizedEnv kept %q", entry)
			}
		}
	}
}

// writeGuardShim writes a command that records its use in sentinel and then
// fails, so a stray wrapper run cannot act on the host (and the test can prove
// the PATH shim was consulted).
func writeGuardShim(t *testing.T, path, sentinel string) {
	t.Helper()
	script := "#!/bin/sh\n: > \"" + sentinel + "\"\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write guard shim %s: %v", path, err)
	}
}

// writeShim writes a fake command that exits with code.
func writeShim(t *testing.T, path string, code int) {
	t.Helper()
	script := "#!/bin/sh\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim %s: %v", path, err)
	}
}

// writeSystemctlShim writes a fake systemctl that optionally snapshots the
// status directory listing (WRAPPER_TEST_STATUS_LIST) so a test can observe
// what the wrapper left there while it held the lock, then exits with code.
func writeSystemctlShim(t *testing.T, path string, code int) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ -n \"${WRAPPER_TEST_STATUS_LIST:-}\" ] && [ -n \"${WRAPPER_TEST_STATUS_DIR:-}\" ]; then\n" +
		"  ls -A \"${WRAPPER_TEST_STATUS_DIR}\" > \"${WRAPPER_TEST_STATUS_LIST}\" 2>/dev/null || true\n" +
		"fi\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write systemctl shim %s: %v", path, err)
	}
}

// writeFlockSwapShim writes a fake flock that simulates the service user
// replacing the lock path while root waits on it: it swaps the lock file and
// exits 0 as if the lock had been acquired.
func writeFlockSwapShim(t *testing.T, path string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ -n \"${WRAPPER_TEST_LOCK:-}\" ]; then\n" +
		"  rm -f \"${WRAPPER_TEST_LOCK}\" 2>/dev/null || true\n" +
		"  : > \"${WRAPPER_TEST_LOCK}\" 2>/dev/null || true\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write flock swap shim %s: %v", path, err)
	}
}

// writeRateLimitShim writes a fake systemctl that models systemd's start rate
// limit: the first restart succeeds, later restarts fail with "Start request
// repeated too quickly" until reset-failed clears the failed state. Every call
// is appended to logPath so a test can assert reset-failed was invoked.
func writeRateLimitShim(t *testing.T, path, logPath string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"log='" + logPath + "'\n" +
		"printf '%s %s\\n' \"${1:-}\" \"${2:-}\" >>\"${log}\" 2>/dev/null || true\n" +
		"case \"${1:-}\" in\n" +
		"  reset-failed)\n" +
		"    rm -f \"${log}.limited\"\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"  restart)\n" +
		"    if [ -f \"${log}.limited\" ]; then\n" +
		"      echo \"Job for ${2:-} failed because start of the service was attempted too often.\" >&2\n" +
		"      echo \"Start request repeated too quickly.\" >&2\n" +
		"      exit 1\n" +
		"    fi\n" +
		"    : >\"${log}.limited\"\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write rate-limit systemctl shim: %v", err)
	}
}

// writeCurlShim writes the fake health probe.
func writeCurlShim(t *testing.T, path, health string) {
	t.Helper()
	var script string
	switch health {
	case healthAlwaysOK:
		script = "#!/bin/sh\nexit 0\n"
	case healthAlwaysFail:
		script = "#!/bin/sh\nexit 1\n"
	case healthAfterBackup:
		script = "#!/bin/sh\n" +
			"if [ -n \"${WRAPPER_TEST_TARGET:-}\" ] && grep -q '^old$' \"${WRAPPER_TEST_TARGET}\" 2>/dev/null; then exit 0; fi\n" +
			"exit 1\n"
	default:
		t.Fatalf("unknown health mode %q", health)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write curl shim: %v", err)
	}
}

// TestWrapperRollsBackWhenRestartFails reproduces the reviewer's repro: a
// failing systemctl restart must still restore the old binary and exit
// nonzero.
func TestWrapperRollsBackWhenRestartFails(t *testing.T) {
	result := runWrapper(t, wrapperEnv{systemctlExit: 1, health: healthAlwaysOK})

	if result.exitCode == 0 {
		t.Fatalf("exit = 0, want nonzero (output %q)", result.output)
	}
	if result.target != "old" {
		t.Fatalf("binary = %q, want the restored old binary (output %q)", result.target, result.output)
	}
	if result.backup != "" {
		t.Errorf("backup still present after rollback: %q", result.backup)
	}
	if !strings.Contains(result.status, "result=rolled_back") {
		t.Errorf("status = %q, want rolled_back", result.status)
	}
	if result.pending != "" {
		t.Errorf("pending marker not released: %q", result.pending)
	}
}

// TestWrapperRestartFailRollbackUnhealthy proves the failed-restart branch
// health-checks the restored binary (item 6 / R7): when the first restart fails
// and the restored binary is also unhealthy, the outcome is rollback_failed,
// not a false rolled_back.
func TestWrapperRestartFailRollbackUnhealthy(t *testing.T) {
	result := runWrapper(t, wrapperEnv{systemctlExit: 1, health: healthAlwaysFail})

	if result.exitCode == 0 {
		t.Fatalf("exit = 0, want nonzero (output %q)", result.output)
	}
	if result.target != "old" {
		t.Fatalf("binary = %q, want the restored old binary (output %q)", result.target, result.output)
	}
	if !strings.Contains(result.status, "result=rollback_failed") {
		t.Fatalf("status = %q, want rollback_failed (output %q)", result.status, result.output)
	}
	if strings.Contains(result.status, "result=rolled_back") {
		t.Fatalf("status = %q, want rollback_failed, not rolled_back", result.status)
	}
	if result.pending != "" {
		t.Errorf("pending marker not released: %q", result.pending)
	}
}

// TestWrapperClearsRateLimitOnRollback proves a crash-looping new binary that
// trips systemd's start rate limit does not turn a healthy rollback into
// rollback_failed: the wrapper clears the failed state with reset-failed before
// every restart, so the rollback restart succeeds and the restored binary runs.
func TestWrapperClearsRateLimitOnRollback(t *testing.T) {
	result := runWrapper(t, wrapperEnv{
		systemctlMode: systemctlRateLimit,
		health:        healthAfterBackup,
	})

	if !strings.Contains(result.systemctlLog, "reset-failed fake") {
		t.Fatalf("wrapper did not call reset-failed (log %q, output %q)", result.systemctlLog, result.output)
	}
	if !strings.Contains(result.status, "result=rolled_back") {
		t.Fatalf("status = %q, want rolled_back (output %q)", result.status, result.output)
	}
	if strings.Contains(result.status, "rollback_failed") {
		t.Fatalf("status = %q, want a successful rollback (output %q)", result.status, result.output)
	}
	if result.target != "old" {
		t.Fatalf("binary = %q, want the restored old binary (output %q)", result.target, result.output)
	}
	if result.pending != "" {
		t.Fatalf("pending marker not released: %q", result.pending)
	}
}

// TestWrapperRollsBackWhenHealthFails proves a restarted-but-unhealthy new
// binary is rolled back to the (healthy) previous one.
func TestWrapperRollsBackWhenHealthFails(t *testing.T) {
	result := runWrapper(t, wrapperEnv{systemctlExit: 0, health: healthAfterBackup})

	if result.exitCode == 0 {
		t.Fatalf("exit = 0, want nonzero (output %q)", result.output)
	}
	if result.target != "old" {
		t.Fatalf("binary = %q, want the restored old binary (output %q)", result.target, result.output)
	}
	if !strings.Contains(result.status, "result=rolled_back") {
		t.Errorf("status = %q, want rolled_back", result.status)
	}
}

// TestWrapperReportsRollbackFailed proves that when neither binary is healthy
// the outcome is recorded as rollback_failed rather than a false ok.
func TestWrapperReportsRollbackFailed(t *testing.T) {
	result := runWrapper(t, wrapperEnv{systemctlExit: 0, health: healthAlwaysFail})

	if result.exitCode == 0 {
		t.Fatalf("exit = 0, want nonzero (output %q)", result.output)
	}
	if !strings.Contains(result.status, "result=rollback_failed") {
		t.Errorf("status = %q, want rollback_failed", result.status)
	}
}

// TestWrapperKeepsHealthyBinary proves a healthy new binary is kept and the
// backup is retained.
func TestWrapperKeepsHealthyBinary(t *testing.T) {
	result := runWrapper(t, wrapperEnv{systemctlExit: 0, health: healthAlwaysOK})

	if result.exitCode != 0 {
		t.Fatalf("exit = %d, want 0 (output %q)", result.exitCode, result.output)
	}
	if result.target != "new" {
		t.Fatalf("binary = %q, want the new binary", result.target)
	}
	if result.backup != "old" {
		t.Errorf("backup = %q, want the retained previous binary", result.backup)
	}
	if !strings.Contains(result.status, "result=ok") {
		t.Errorf("status = %q, want ok", result.status)
	}
}

// TestWrapperRecoversMissingTarget proves the wrapper restores the backup when
// the target is missing (an older crash layout), so ExecStart always finds a
// binary.
func TestWrapperRecoversMissingTarget(t *testing.T) {
	result := runWrapper(t, wrapperEnv{
		systemctlExit: 0,
		health:        healthAlwaysOK,
		setup: func(_ string, target, _ string) {
			if err := os.Remove(target); err != nil {
				t.Fatalf("remove target: %v", err)
			}
		},
	})

	if result.exitCode == 0 {
		t.Fatalf("exit = 0, want nonzero (recovered, output %q)", result.output)
	}
	if result.target != "old" {
		t.Fatalf("binary = %q, want the recovered old binary (output %q)", result.target, result.output)
	}
	if !strings.Contains(result.output, "restoring") {
		t.Errorf("output = %q, want a recovery message", result.output)
	}
}

// TestWrapperRefusesSymlinkedStatus reproduces the reviewer's root-file clobber:
// a symlinked status path planted by the service user must not be followed.
func TestWrapperRefusesSymlinkedStatus(t *testing.T) {
	victimDir := t.TempDir()
	victim := filepath.Join(victimDir, "victim")
	if err := os.WriteFile(victim, []byte("original"), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	result := runWrapper(t, wrapperEnv{
		systemctlExit: 1,
		health:        healthAlwaysOK,
		setup: func(_ string, _ string, statusPath string) {
			if err := os.Remove(statusPath); err != nil && !os.IsNotExist(err) {
				t.Fatalf("remove status placeholder: %v", err)
			}
			if err := os.Symlink(victim, statusPath); err != nil {
				t.Fatalf("plant symlink: %v", err)
			}
		},
	})

	if got := readFile(t, victim); got != "original" {
		t.Fatalf("victim = %q, want it untouched", got)
	}
	if !strings.Contains(result.output, "refusing symlinked status") {
		t.Errorf("output = %q, want a refusal message", result.output)
	}
}

// TestWrapperStatusWriteIsSafe is a static guard: the status temp file must be
// created with mktemp (O_EXCL), never a predictable `${STATUS}.tmp.$$`.
func TestWrapperStatusWriteIsSafe(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	body := string(script)
	if !strings.Contains(body, "mktemp") {
		t.Error("wrapper does not create the status temp file with mktemp")
	}
	if strings.Contains(body, ".tmp.$$") {
		t.Error("wrapper still uses the predictable ${STATUS}.tmp.$$ path")
	}
	if !strings.Contains(body, `[ "$#" -ne 0 ]`) {
		t.Error("wrapper does not reject arguments")
	}
	if !strings.Contains(body, "SUDO_USER") {
		t.Error("wrapper does not refuse environment overrides under sudo")
	}
	if !strings.Contains(body, `exec 9<`) {
		t.Error("wrapper does not open the lock read-only")
	}
	if strings.Contains(body, `exec 9>`) {
		t.Error("wrapper still opens the lock for writing (truncation risk)")
	}
	if !strings.Contains(body, "status_dir_usable") {
		t.Error("wrapper does not verify the root-owned status directory before writing/opening")
	}
	if !strings.Contains(body, ".update-lock.") {
		t.Error("wrapper does not pin the lock inode into the root-owned status directory")
	}
}

// TestWrapperRefusesSymlinkedLock reproduces N1: a Gotham-planted update.lock
// symlink must not make root truncate/modify the target.
func TestWrapperRefusesSymlinkedLock(t *testing.T) {
	victimDir := t.TempDir()
	victim := filepath.Join(victimDir, "victim-root-only")
	if err := os.WriteFile(victim, []byte("root-only contents"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}

	result := runWrapper(t, wrapperEnv{
		systemctlExit: 0,
		health:        healthAlwaysOK,
		setup: func(dir, _ string, _ string) {
			lock := filepath.Join(dir, "update.lock")
			if err := os.Remove(lock); err != nil && !os.IsNotExist(err) {
				t.Fatalf("remove lock: %v", err)
			}
			if err := os.Symlink(victim, lock); err != nil {
				t.Fatalf("plant lock symlink: %v", err)
			}
		},
	})

	if got := readFile(t, victim); got != "root-only contents" {
		t.Fatalf("victim = %q (size %d), want it byte-identical", got, len(got))
	}
	if !strings.Contains(result.output, "refusing to lock") {
		t.Errorf("output = %q, want a lock refusal", result.output)
	}
	if !strings.Contains(result.status, "result=wrapper_failed") {
		t.Errorf("status = %q, want wrapper_failed", result.status)
	}
}

// TestWrapperPinsLockInode proves the lock inode is pinned into the root-owned
// status directory before it is opened: the fake systemctl snapshots the
// directory while the wrapper holds the lock, and the pin must be there. The
// pin is what stops a swapped-in FIFO from blocking root's open(2); a skipped
// pin would silently fall back to the racy open.
func TestWrapperPinsLockInode(t *testing.T) {
	listing := filepath.Join(t.TempDir(), "status-listing")
	t.Setenv("WRAPPER_TEST_STATUS_LIST", listing)

	runWrapper(t, wrapperEnv{systemctlExit: 0, health: healthAlwaysOK})

	data, err := os.ReadFile(listing)
	if err != nil {
		t.Fatalf("read the status dir snapshot: %v", err)
	}
	if !strings.Contains(string(data), ".update-lock.") {
		t.Fatalf("status dir during the update = %q, want a pinned lock hardlink", data)
	}
}

// TestWrapperRemovesLockPin proves the pin is transient: it is removed when the
// wrapper exits, so it cannot accumulate in the root-owned directory.
func TestWrapperRemovesLockPin(t *testing.T) {
	result := runWrapper(t, wrapperEnv{systemctlExit: 0, health: healthAlwaysOK})

	entries, err := os.ReadDir(result.statusDir)
	if err != nil {
		t.Fatalf("read status dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".update-lock.") {
			t.Fatalf("lock pin %s survived the wrapper", entry.Name())
		}
	}
}

// TestWrapperRefusesLockSwappedWhileWaiting proves the post-flock inode check:
// when the lock path is replaced while root waits on it, the wrapper refuses
// instead of running the update while the control plane locks a different
// inode.
func TestWrapperRefusesLockSwappedWhileWaiting(t *testing.T) {
	result := runWrapper(t, wrapperEnv{
		systemctlExit: 0,
		health:        healthAlwaysOK,
		flockShim:     flockSwapWhileWaiting,
	})

	if !strings.Contains(result.output, "lock") || !strings.Contains(result.output, "changed") {
		t.Errorf("output = %q, want a lock-changed refusal", result.output)
	}
	if !strings.Contains(result.status, "result=wrapper_failed") {
		t.Fatalf("status = %q, want wrapper_failed", result.status)
	}
	if result.target != "new" {
		t.Errorf("binary = %q, want the update refused before any restart/rollback", result.target)
	}
}

// TestWrapperRefusesStatusDirOutsideRootOwnership proves the status directory
// floor is enforced at run time: a symlinked status directory (a shape the
// installer never produces) is refused, the update still runs, and no
// authoritative status is written into the unsafe path.
func TestWrapperRefusesStatusDirOutsideRootOwnership(t *testing.T) {
	result := runWrapper(t, wrapperEnv{
		systemctlExit: 0,
		health:        healthAlwaysOK,
		setup: func(dir, _ string, _ string) {
			statusDir := filepath.Join(dir, "statusdir")
			if err := os.RemoveAll(statusDir); err != nil {
				t.Fatalf("remove statusdir: %v", err)
			}
			target := filepath.Join(dir, "real-statusdir")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatalf("mkdir real-statusdir: %v", err)
			}
			if err := os.Symlink(target, statusDir); err != nil {
				t.Fatalf("symlink statusdir: %v", err)
			}
		},
	})

	if !strings.Contains(result.output, "not a root-owned directory") {
		t.Errorf("output = %q, want a status-directory refusal", result.output)
	}
	if result.status != "" {
		t.Errorf("status = %q, want no status written through the symlinked directory", result.status)
	}
}

// TestWrapperHardensPendingRead reproduces L1: a symlinked or FIFO pending
// marker must not leak a root-only version line or hang root.
func TestWrapperHardensPendingRead(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		victimDir := t.TempDir()
		victim := filepath.Join(victimDir, "secret")
		if err := os.WriteFile(victim, []byte("version=ROOT-SECRET\n"), 0o600); err != nil {
			t.Fatalf("write victim: %v", err)
		}
		result := runWrapper(t, wrapperEnv{
			systemctlExit: 0,
			health:        healthAlwaysOK,
			setup: func(dir, _ string, _ string) {
				pending := filepath.Join(dir, "update.pending")
				if err := os.Remove(pending); err != nil && !os.IsNotExist(err) {
					t.Fatalf("remove pending: %v", err)
				}
				if err := os.Symlink(victim, pending); err != nil {
					t.Fatalf("plant pending symlink: %v", err)
				}
			},
		})
		if strings.Contains(result.status, "ROOT-SECRET") {
			t.Fatalf("status leaked the root-only version: %q", result.status)
		}
		if got := readFile(t, victim); got != "version=ROOT-SECRET\n" {
			t.Errorf("victim = %q, want it unchanged", got)
		}
	})

	t.Run("fifo", func(t *testing.T) {
		result := runWrapper(t, wrapperEnv{
			systemctlExit: 0,
			health:        healthAlwaysOK,
			setup: func(dir, _ string, _ string) {
				pending := filepath.Join(dir, "update.pending")
				if err := os.Remove(pending); err != nil && !os.IsNotExist(err) {
					t.Fatalf("remove pending: %v", err)
				}
				if err := syscall.Mkfifo(pending, 0o644); err != nil {
					t.Fatalf("mkfifo: %v", err)
				}
			},
		})
		// A hang would trip the harness timeout; reaching here proves it did not.
		if !strings.Contains(result.status, "result=ok") {
			t.Fatalf("status = %q, want ok (no hang, no FIFO read)", result.status)
		}
	})
}

// TestReadRegularOrEmptyNeverBlocks proves the harness read helper returns
// promptly for FIFOs, symlinks, directories and missing paths (os.ReadFile on a
// FIFO would block forever).
func TestReadRegularOrEmptyNeverBlocks(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	symlink := filepath.Join(dir, "symlink")
	if err := os.Symlink("/nonexistent-target", symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("data"), 0o644); err != nil {
		t.Fatalf("write regular: %v", err)
	}

	cases := []struct {
		path string
		want string
	}{
		{fifo, ""},
		{symlink, ""},
		{subdir, ""},
		{filepath.Join(dir, "missing"), ""},
		{regular, "data"},
	}
	for _, tc := range cases {
		done := make(chan string, 1)
		go func() { done <- readRegularOrEmpty(tc.path) }()
		select {
		case got := <-done:
			if got != tc.want {
				t.Errorf("readRegularOrEmpty(%s) = %q, want %q", tc.path, got, tc.want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("readRegularOrEmpty(%s) blocked", tc.path)
		}
	}
}

// TestRunWrapperDoesNotBlockOnNonRegularPaths plants a FIFO or a symlink at
// every path runWrapper reads and asserts it returns within a short deadline; a
// planted FIFO must never hang the harness (the round-6 CI hang).
func TestRunWrapperDoesNotBlockOnNonRegularPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gotham-update.sh targets POSIX")
	}
	cases := []struct {
		name string
		path func(dir, target, status string) string
		fifo bool
	}{
		{"pending-fifo", func(dir, _, _ string) string { return filepath.Join(dir, "update.pending") }, true},
		{"pending-symlink", func(dir, _, _ string) string { return filepath.Join(dir, "update.pending") }, false},
		{"status-fifo", func(_, _, status string) string { return status }, true},
		{"status-symlink", func(_, _, status string) string { return status }, false},
		{"target-fifo", func(_, target, _ string) string { return target }, true},
		{"backup-fifo", func(_, target, _ string) string { return target + OldSuffix }, true},
		{"lock-fifo", func(dir, _, _ string) string { return filepath.Join(dir, "update.lock") }, true},
		{"lock-symlink", func(dir, _, _ string) string { return filepath.Join(dir, "update.lock") }, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setup := func(dir, target, status string) {
				if tc.fifo {
					plantFIFO(t, tc.path(dir, target, status))
				} else {
					plantSymlink(t, tc.path(dir, target, status))
				}
			}
			done := make(chan struct{}, 1)
			go func() {
				runWrapper(t, wrapperEnv{
					systemctlExit: 0,
					health:        healthAlwaysOK,
					setup:         setup,
				})
				done <- struct{}{}
			}()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Fatal("runWrapper blocked on a non-regular path")
			}
		})
	}
}

// plantFIFO replaces path with a FIFO.
func plantFIFO(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove %s: %v", path, err)
	}
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatalf("mkfifo %s: %v", path, err)
	}
}

// plantSymlink replaces path with a dangling symlink.
func plantSymlink(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove %s: %v", path, err)
	}
	if err := os.Symlink("/nonexistent-target", path); err != nil {
		t.Fatalf("symlink %s: %v", path, err)
	}
}

// TestWrapperRemovesNonRegularPendingOnFailClosed proves a planted FIFO pending
// marker is removed even when the wrapper fails closed on the lock, so it can
// never hang a later reader (the round-6 CI hang).
func TestWrapperRemovesNonRegularPendingOnFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gotham-update.sh targets POSIX")
	}
	var pending string
	runWrapper(t, wrapperEnv{
		systemctlExit: 0,
		health:        healthAlwaysOK,
		setup: func(dir, _ string, _ string) {
			pending = filepath.Join(dir, "update.pending")
			plantFIFO(t, pending)
			lock := filepath.Join(dir, "update.lock")
			if err := os.Remove(lock); err != nil && !os.IsNotExist(err) {
				t.Fatalf("remove lock: %v", err)
			}
			if err := os.Symlink("/nonexistent-target", lock); err != nil {
				t.Fatalf("plant lock symlink: %v", err)
			}
		},
	})

	info, err := os.Lstat(pending)
	if err == nil && info.Mode()&os.ModeNamedPipe != 0 {
		t.Fatalf("wrapper left a FIFO pending marker after failing closed")
	}
}

// TestWrapperRejectsArguments proves the no-argument contract.
func TestWrapperRejectsArguments(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	cmd := exec.Command("sh", script, "unexpected")
	cmd.Env = sanitizedEnv()
	output, runErr := cmd.CombinedOutput()
	exitErr, ok := runErr.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("exit = %v, want 2 (output %q)", runErr, output)
	}
	if !strings.Contains(string(output), "takes no arguments") {
		t.Errorf("output = %q, want a no-arguments message", output)
	}
}

// TestWrapperIgnoresEnvUnderSudo proves a GOTHAM_* override cannot change the
// wrapper's configuration when it runs through sudo. It never touches a real
// install: it skips when one is present, and every command the wrapper could
// use to act on the host is replaced by a failing shim that records its use.
func TestWrapperIgnoresEnvUnderSudo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gotham-update.sh targets POSIX")
	}
	if realInstallPresent() {
		t.Skip("a real Gotham install is present; refusing to run the wrapper against it")
	}

	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fakebin: %v", err)
	}
	sentinel := filepath.Join(dir, "shim-used")
	for _, name := range []string{"systemctl", "flock", "curl", "wget", "mv", "mkdir", "mktemp", "chmod", "rm", "install"} {
		writeGuardShim(t, filepath.Join(fakeBin, name), sentinel)
	}

	conf := filepath.Join(dir, "evil.conf")
	if err := os.WriteFile(conf, []byte("GOTHAM_HEALTH=http://evil.example.com/healthz\n"), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}

	cmd := exec.Command("sh", script)
	cmd.Env = append(sanitizedEnv(),
		"SUDO_USER=root",
		"SUDO_UID=0",
		"SUDO_GID=0",
		"GOTHAM_UPDATER_CONF="+conf,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, _ := cmd.CombinedOutput()

	if strings.Contains(string(output), "evil.example.com") || strings.Contains(string(output), "must be a loopback") {
		t.Fatalf("wrapper honoured the GOTHAM_UPDATER_CONF override under sudo: %q", output)
	}
	if !strings.Contains(string(output), "refusing to lock") {
		t.Fatalf("wrapper did not fall back to the default lock path: %q", output)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("PATH shims were not used (no sentinel): %v", err)
	}
	for _, path := range []string{"/var/lib/gotham", "/var/lib/gotham-updater"} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("wrapper created a real install path %s", path)
		}
	}
}
