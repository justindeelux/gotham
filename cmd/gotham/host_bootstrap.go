package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/justindeelux/gotham/internal/hostbootstrap"
)

// runHostBootstrap installs and refreshes the fixed, root-owned host helper and
// host-tool installer together with their verb-pinned sudoers drop-ins. It is
// run as root by the control-plane unit (ExecStartPre=+) and reads no
// configuration file or environment: fixed paths, fixed embedded content, and
// only these fixed flags.
func runHostBootstrap(args []string) int {
	fs := flag.NewFlagSet("host-bootstrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cpUser := fs.String("cp-user", hostbootstrap.DefaultCpUser, "control-plane service user granted the host-helper verbs")
	agentUser := fs.String("agent-user", hostbootstrap.DefaultAgentUser, "node-agent service user granted the tool-installer verbs")
	root := fs.String("root", "", "write a test root prefix instead of / (empty = real root)")
	visudo := fs.String("visudo", "", "visudo path used to validate the sudoers drop-ins (empty = resolve on PATH)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := hostbootstrap.Run(hostbootstrap.Options{
		CpUser:    *cpUser,
		AgentUser: *agentUser,
		Root:      *root,
		Visudo:    *visudo,
		Logf:      func(format string, a ...any) { logger.Info(fmt.Sprintf(format, a...)) },
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "host-bootstrap: %v\n", err)
		return exitError
	}
	return exitOK
}
