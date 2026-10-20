package main

import (
	"testing"

	"github.com/justindeelux/gotham/agent"
)

// TestNewTerminalConfigMapsConfig proves the agent config is carried into the
// terminal service, including the GOTHAM_AGENT_TERMINAL_INTEGRATION off switch
// (TerminalIntegration -> IntegrationDisabled).
func TestNewTerminalConfigMapsConfig(t *testing.T) {
	cfg := agent.Config{
		TerminalShell:       "/bin/zsh",
		TerminalMaxSessions: 3,
		TerminalIntegration: true,
	}
	enabled := newTerminalConfig(cfg, nil, nil)
	if enabled.Shell != cfg.TerminalShell || enabled.MaxSessions != cfg.TerminalMaxSessions {
		t.Fatalf("terminal config = %+v; want shell %q and max sessions %d", enabled, cfg.TerminalShell, cfg.TerminalMaxSessions)
	}
	if enabled.IntegrationDisabled {
		t.Fatal("IntegrationDisabled = true; want the integration on by default")
	}
	if enabled.Roots != nil || enabled.Containers != nil {
		t.Fatal("newTerminalConfig must leave Roots and a nil Containers unset")
	}

	cfg.TerminalIntegration = false
	disabled := newTerminalConfig(cfg, nil, nil)
	if !disabled.IntegrationDisabled {
		t.Fatal("IntegrationDisabled = false; want GOTHAM_AGENT_TERMINAL_INTEGRATION=false to reach the terminal service")
	}
}

// TestNewTerminalConfigHonoursIntegrationEnv proves the environment switch is
// carried end to end: Load reads GOTHAM_AGENT_TERMINAL_INTEGRATION and the
// config-to-server mapping disables the integration.
func TestNewTerminalConfigHonoursIntegrationEnv(t *testing.T) {
	t.Setenv("GOTHAM_AGENT_CA", "/etc/gotham/ca.pem")
	t.Setenv("GOTHAM_AGENT_TERMINAL_INTEGRATION", "false")
	cfg, err := agent.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TerminalIntegration {
		t.Fatal("Config.TerminalIntegration = true; GOTHAM_AGENT_TERMINAL_INTEGRATION=false must disable it")
	}
	if !newTerminalConfig(cfg, nil, nil).IntegrationDisabled {
		t.Fatal("the terminal service did not receive the disabled integration")
	}
}
