package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/servers"
)

// caCertFileName is the CA certificate `gotham ca init` writes. It is the file
// an operator copies to each node and passes to install-agent.sh --ca.
const caCertFileName = "ca.crt"

// runCA implements `gotham ca init`: it provisions the gRPC mTLS certificate
// authority under the configured GOTHAM_CA_DIR. It is idempotent, so the
// installer can run it on every (re)install; an existing CA is loaded, never
// replaced.
func runCA(args []string) int {
	if len(args) == 0 {
		caUsage(os.Stderr)
		return exitUsage
	}
	switch args[0] {
	case "init":
		return runCAInit()
	case "help", "-h", "--help":
		caUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown ca command %q\n\n", args[0])
		caUsage(os.Stderr)
		return exitUsage
	}
}

// runCAInit creates the CA when it is missing and reports the certificate path.
func runCAInit() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return exitError
	}
	dir := cfg.Snapshot().CA.Dir
	if dir == "" {
		fmt.Fprintln(os.Stderr, "ca init: GOTHAM_CA_DIR must not be empty")
		return exitError
	}
	if _, err := servers.LoadOrCreateAuthority(dir); err != nil {
		fmt.Fprintf(os.Stderr, "ca init: %v\n", err)
		return exitError
	}
	fmt.Printf("certificate authority ready: %s\n", filepath.Join(dir, caCertFileName))
	fmt.Println("Copy ca.crt to each node and install the agent with: install-agent.sh --ca <ca.crt>")
	return exitOK
}

// caUsage prints the `gotham ca` help.
func caUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham ca <command>

Commands:
  init                  Create the gRPC mTLS certificate authority under GOTHAM_CA_DIR

GOTHAM_CA_DIR (default ./data/ca/) holds ca.crt (public) and ca.key (private,
0600). The installer runs this once, and gotham serve loads it to run the gRPC
gateway over TLS.
`)
}
