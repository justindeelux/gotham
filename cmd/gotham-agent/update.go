package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justindeelux/gotham/agent"
)

func runUpdate(args []string) int {
	if len(args) == 0 {
		updateUsage(os.Stderr)
		return exitUsage
	}

	loadAgentEnvFile()

	cfg, err := agent.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}

	switch args[0] {
	case "reset":
		if err := agent.ResetUpdateState(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "update reset: %v\n", err)
			return exitError
		}
		fmt.Println("cleared the failed-update state; the agent will retry on its next poll")
		return exitOK
	case "help", "-h", "--help":
		updateUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown update command %q\n\n", args[0])
		updateUsage(os.Stderr)
		return exitUsage
	}
}

func loadAgentEnvFile() {
	path := os.Getenv("GOTHAM_AGENT_ENV_FILE")
	if path == "" {
		path = "/etc/gotham/agent.env"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !strings.HasPrefix(key, "GOTHAM_") {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, unquoteEnvValue(strings.TrimSpace(value)))
	}
}

func unquoteEnvValue(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func updateUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham-agent update <command>

Commands:
  reset    Clear the failed-update state and backoff so a fixed release is retried

A newer release is applied automatically; reset is only needed to retry the same
version after a rollback. Paths are resolved from the process environment, then
/etc/gotham/agent.env (GOTHAM_AGENT_ENV_FILE overrides the file), then the
built-in defaults. Run it as root to also remove the root-owned status file (a
non-root reset still clears the backoff through the agent-owned retry marker).
`)
}
