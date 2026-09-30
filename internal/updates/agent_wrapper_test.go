package updates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runAgentWrapper runs the shared wrapper installed under the agent name
// (gotham-agent-update) with fake systemctl/curl/flock shims, against an
// agent-shaped configuration. It never touches a real install.
func runAgentWrapper(t *testing.T, systemctlExit int, health string, withConf bool) wrapperResult {
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
	writeShim(t, filepath.Join(fakeBin, "flock"), 0)

	target := filepath.Join(dir, "bin", "gotham-agent")
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

	conf := filepath.Join(dir, "agent-updater.conf")
	if withConf {
		confBody := strings.Join([]string{
			"GOTHAM_BINARY=" + target,
			"GOTHAM_SERVICE=gotham-agent",
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
	}

	// Install the shared wrapper under the agent name so it selects the agent
	// default configuration branch.
	source, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	wrapper := filepath.Join(dir, "gotham-agent-update")
	if err := os.WriteFile(wrapper, body, 0o755); err != nil {
		t.Fatalf("write agent wrapper: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", wrapper)
	cmd.Env = append(sanitizedEnv(),
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
	result.target = readRegularOrEmpty(target)
	result.backup = readRegularOrEmpty(backup)
	result.status = readRegularOrEmpty(status)
	result.pending = readRegularOrEmpty(pending)
	return result
}

// TestAgentWrapperRestartsAndRecordsOK proves the agent-named wrapper restarts
// the fixed unit and records a healthy outcome.
func TestAgentWrapperRestartsAndRecordsOK(t *testing.T) {
	result := runAgentWrapper(t, 0, healthAlwaysOK, true)
	if result.exitCode != 0 {
		t.Fatalf("exit = %d, want 0; output=%q", result.exitCode, result.output)
	}
	if !strings.Contains(result.status, "result=ok") {
		t.Fatalf("status = %q, want result=ok", result.status)
	}
	if result.pending != "" {
		t.Fatalf("pending = %q, want it cleared", result.pending)
	}
}

// TestAgentWrapperRollsBackOnFailedRestart proves a failed restart restores the
// previous binary and records rolled_back.
func TestAgentWrapperRollsBackOnFailedRestart(t *testing.T) {
	result := runAgentWrapper(t, 1, healthAlwaysOK, true)
	if result.exitCode == 0 {
		t.Fatalf("exit = 0, want nonzero; output=%q", result.output)
	}
	if !strings.Contains(result.status, "result=rolled_back") {
		t.Fatalf("status = %q, want result=rolled_back", result.status)
	}
	if result.target != "old" {
		t.Fatalf("target = %q, want the restored old binary", result.target)
	}
}

// TestAgentWrapperFailsClosedWithoutConf proves the agent wrapper refuses to
// run without its root-owned configuration instead of falling back to the
// control-plane defaults.
func TestAgentWrapperFailsClosedWithoutConf(t *testing.T) {
	result := runAgentWrapper(t, 0, healthAlwaysOK, false)
	if result.exitCode != 2 {
		t.Fatalf("exit = %d, want 2 (invalid/missing configuration); output=%q", result.exitCode, result.output)
	}
	if result.target != "new" {
		t.Fatalf("target = %q, want it untouched", result.target)
	}
}

// TestAgentWrapperRefusesArguments pins the no-argument contract the sudoers
// rule relies on.
func TestAgentWrapperRefusesArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gotham-update.sh targets POSIX")
	}
	dir := t.TempDir()
	source, err := filepath.Abs(filepath.Join("..", "..", "deploy", "gotham-update.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	wrapper := filepath.Join(dir, "gotham-agent-update")
	if err := os.WriteFile(wrapper, body, 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	cmd := exec.Command("sh", wrapper, "--force")
	cmd.Env = sanitizedEnv()
	output, runErr := cmd.CombinedOutput()
	exitErr, ok := runErr.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("wrapper with an argument = %v (%q), want exit 2", runErr, output)
	}
	if !strings.Contains(string(output), "takes no arguments") {
		t.Fatalf("output = %q, want the no-arguments refusal", output)
	}
}

// TestAgentUnitWiring pins the unit settings the update chain depends on:
// KillMode=process (the wrapper must survive the restart cgroup), the fixed
// binary path, the update paths, and the absence of NoNewPrivileges (the agent
// needs setuid sudo for the fixed wrapper).
func TestAgentUnitWiring(t *testing.T) {
	unit, err := os.ReadFile(filepath.Join("..", "..", "deploy", "gotham-agent.service"))
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	body := string(unit)
	for _, want := range []string{
		"KillMode=process",
		"GOTHAM_AGENT_BINARY=",
		"GOTHAM_AGENT_UPDATE_PENDING=",
		"GOTHAM_AGENT_UPDATE_LOCK=",
		"ExecStart=/var/lib/gotham-agent/bin/gotham-agent serve",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("agent unit is missing %q", want)
		}
	}
	if strings.Contains(body, "NoNewPrivileges=true") {
		t.Error("agent unit must not set NoNewPrivileges=true: the fixed wrapper runs through sudo")
	}
}
