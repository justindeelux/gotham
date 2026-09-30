package main

import (
	"fmt"
	"io"
	"os"

	"github.com/justindeelux/gotham/agent"
)

// runUpdate implements `gotham-agent update <reset>`: the operator path for the
// agent's self-update state. The agent applies a newer release automatically;
// reset is only needed to retry the *same* version after a rollback (for
// example after a broken release was re-published with a fix).
func runUpdate(args []string) int {
	if len(args) == 0 {
		updateUsage(os.Stderr)
		return exitUsage
	}

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

// updateUsage prints the `gotham-agent update` help.
func updateUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham-agent update <command>

Commands:
  reset    Clear the failed-update state and backoff so a fixed release is retried

A newer release is applied automatically; reset is only needed to retry the same
version after a rollback. Run it as root to also remove the root-owned status
file (a non-root reset still clears the backoff through the agent-owned retry
marker).
`)
}
