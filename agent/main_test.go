package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var agentTestSandbox string

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
		"GOTHAM_AGENT_UPDATE_BACKOFF": filepath.Join(sandbox, "update.backoff"),
		"GOTHAM_AGENT_AUTO_UPDATE":    "false",
		"GOTHAM_UPDATE_PUBLIC_KEY":    "",
		"GOTHAM_AGENT_INSECURE":       "true",
		"GOTHAM_AGENT_LISTEN_ADDR":    "127.0.0.1:9443",
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
		"UpdateBackoffPath": cfg.UpdateBackoffPath,
	} {
		if !strings.HasPrefix(path, agentTestSandbox) {
			t.Fatalf("%s = %q escapes the sandbox %q", name, path, agentTestSandbox)
		}
	}
	if cfg.AutoUpdate {
		t.Error("AutoUpdate = true; the sandbox must leave unattended updates off")
	}
}
