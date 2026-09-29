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
	output   string
}

// health modes for the fake curl shim.
const (
	healthAlwaysOK    = "ok"             // the service is always healthy
	healthAlwaysFail  = "fail"           // the service is never healthy
	healthAfterBackup = "after-rollback" // healthy only once the old binary is back
)

// runWrapper runs the deployed wrapper with fake systemctl/curl shims. It
// reproduces the reviewer's repro: a restart that exits nonzero must still
// restore the previous binary.
func runWrapper(t *testing.T, systemctlExit int, health string) wrapperResult {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gotham-update.sh targets POSIX")
	}

	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fakebin: %v", err)
	}
	writeShim(t, filepath.Join(fakeBin, "systemctl"), systemctlExit)
	writeCurlShim(t, filepath.Join(fakeBin, "curl"), health)

	target := filepath.Join(dir, "gotham")
	if err := os.WriteFile(target, []byte("new"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	backup := target + OldSuffix
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	status := filepath.Join(dir, "run", "update.status")
	conf := filepath.Join(dir, "updater.conf")
	confBody := strings.Join([]string{
		"GOTHAM_BINARY=" + target,
		"GOTHAM_SERVICE=fake",
		"GOTHAM_HEALTH=http://127.0.0.1:1/healthz",
		"GOTHAM_TIMEOUT=1",
		"GOTHAM_STATUS=" + status,
		"GOTHAM_GRACE=0",
	}, "\n") + "\n"
	if err := os.WriteFile(conf, []byte(confBody), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
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
	result := runWrapper(t, 1, healthAlwaysOK)

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
}

// TestWrapperRollsBackWhenHealthFails proves a restarted-but-unhealthy new
// binary is rolled back to the (healthy) previous one.
func TestWrapperRollsBackWhenHealthFails(t *testing.T) {
	result := runWrapper(t, 0, healthAfterBackup)

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
	result := runWrapper(t, 0, healthAlwaysFail)

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
	result := runWrapper(t, 0, healthAlwaysOK)

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
