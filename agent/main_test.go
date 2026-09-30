package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentTestSandbox is the directory every default update path resolves into
// during the test run, so a bug can never point the agent updater at a real
// install (mirrors internal/server's TestMain sandbox).
var agentTestSandbox string

// TestMain points the agent's default update paths at a throwaway sandbox and
// disables unattended auto-update before any test runs. Tests that need the
// surface on still override with t.Setenv, which restores the sandbox after.
func TestMain(m *testing.M) {
	sandbox, err := os.MkdirTemp("", "gotham-agent-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent TestMain: mkdtemp: %v\n", err)
		os.Exit(1)
	}
	agentTestSandbox = sandbox
	for key, value := range map[string]string{
		"GOTHAM_AGENT_BINARY":         filepath.Join(sandbox, "bin", "gotham-agent"),
		"GOTHAM_AGENT_UPDATE_SCRIPT":  filepath.Join(sandbox, "gotham-agent-update"),
		"GOTHAM_AGENT_UPDATE_STATUS":  filepath.Join(sandbox, "status", "update.status"),
		"GOTHAM_AGENT_UPDATE_PENDING": filepath.Join(sandbox, "update.pending"),
		"GOTHAM_AGENT_UPDATE_LOCK":    filepath.Join(sandbox, "update.lock"),
		"GOTHAM_AGENT_UPDATE_RETRY":   filepath.Join(sandbox, "update.retry"),
		"GOTHAM_AGENT_AUTO_UPDATE":    "false",
		"GOTHAM_UPDATE_PUBLIC_KEY":    "",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "agent TestMain: setenv %s: %v\n", key, err)
			os.RemoveAll(sandbox)
			os.Exit(1)
		}
	}

	code := m.Run()
	os.RemoveAll(sandbox)
	os.Exit(code)
}

// TestAgentUpdatePathsSandboxed fails loudly if the default update paths escape
// the sandbox, so removing the TestMain wiring is caught instead of silently
// reaching a real install.
func TestAgentUpdatePathsSandboxed(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for name, path := range map[string]string{
		"BinaryPath":        cfg.BinaryPath,
		"UpdateScript":      cfg.UpdateScript,
		"UpdateStatusPath":  cfg.UpdateStatusPath,
		"UpdatePendingPath": cfg.UpdatePendingPath,
		"UpdateLockPath":    cfg.UpdateLockPath,
		"UpdateRetryPath":   cfg.UpdateRetryPath,
	} {
		if !strings.HasPrefix(path, agentTestSandbox) {
			t.Fatalf("%s = %q escapes the sandbox %q", name, path, agentTestSandbox)
		}
	}
	if cfg.AutoUpdate {
		t.Error("AutoUpdate = true; the sandbox must leave unattended updates off")
	}
}
