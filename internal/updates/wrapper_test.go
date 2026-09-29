package updates

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// wrapperResult captures the outcome of one gotham-update.sh run.
type wrapperResult struct {
	exitCode int
	target   string
	backup   string
	status   string
	pending  string
	output   string
}

// health modes for the fake curl shim.
const (
	healthAlwaysOK    = "ok"             // the service is always healthy
	healthAlwaysFail  = "fail"           // the service is never healthy
	healthAfterBackup = "after-rollback" // healthy only once the old binary is back
)

// wrapperEnv describes one wrapper run.
type wrapperEnv struct {
	systemctlExit int
	health        string
	// setup runs after the default files are created, to plant symlinks etc.
	setup func(dir, target, statusPath string)
}

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
	writeShim(t, filepath.Join(fakeBin, "systemctl"), env.systemctlExit)
	writeCurlShim(t, filepath.Join(fakeBin, "curl"), env.health)

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
	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(),
		"GOTHAM_UPDATER_CONF="+conf,
		"WRAPPER_TEST_TARGET="+target,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, runErr := cmd.CombinedOutput()

	result := wrapperResult{output: string(output)}
	if runErr != nil {
		exitErr, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("run wrapper: %v", runErr)
		}
		result.exitCode = exitErr.ExitCode()
	}
	if data, err := os.ReadFile(target); err == nil {
		result.target = string(data)
	}
	if data, err := os.ReadFile(backup); err == nil {
		result.backup = string(data)
	}
	if data, err := os.ReadFile(status); err == nil {
		result.status = string(data)
	}
	if data, err := os.ReadFile(pending); err == nil {
		result.pending = string(data)
	}
	return result
}

// writeShim writes a fake command that exits with code.
func writeShim(t *testing.T, path string, code int) {
	t.Helper()
	script := "#!/bin/sh\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim %s: %v", path, err)
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
}

// TestWrapperRejectsArguments proves the no-argument contract.
func TestWrapperRejectsArguments(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	cmd := exec.Command("sh", script, "unexpected")
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
// wrapper's configuration when it runs through sudo.
func TestWrapperIgnoresEnvUnderSudo(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "evil.conf")
	if err := os.WriteFile(conf, []byte("GOTHAM_HEALTH=http://evil.example.com/healthz\n"), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(),
		"SUDO_USER=root",
		"SUDO_UID=0",
		"GOTHAM_UPDATER_CONF="+conf,
	)
	output, _ := cmd.CombinedOutput()
	if strings.Contains(string(output), "must be a loopback") {
		t.Fatalf("wrapper honoured the GOTHAM_UPDATER_CONF override under sudo: %q", output)
	}
}
