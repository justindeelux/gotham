package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
		return runCAInit(args[1:])
	case "help", "-h", "--help":
		caUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown ca command %q\n\n", args[0])
		caUsage(os.Stderr)
		return exitUsage
	}
}

// runCAInit creates the CA when it is missing, persists the optional listener
// SAN host list, and reports the certificate path. It is idempotent.
func runCAInit(args []string) int {
	hosts, err := parseHostFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ca init: %v\n\n", err)
		caUsage(os.Stderr)
		return exitUsage
	}

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
	if len(hosts) > 0 {
		if err := servers.SaveHosts(dir, hosts); err != nil {
			fmt.Fprintf(os.Stderr, "ca init: %v\n", err)
			return exitError
		}
	}
	fmt.Printf("certificate authority ready: %s\n", filepath.Join(dir, caCertFileName))
	if len(hosts) > 0 {
		fmt.Printf("gRPC listener certificate hosts: %s\n", strings.Join(hosts, ", "))
	}
	fmt.Println("Copy ca.crt to each node and install the agent with: install-agent.sh --ca <ca.crt>")
	return exitOK
}

// parseHostFlags accepts repeatable --host <name-or-ip> (and --host=) flags and
// splits comma-separated values. Unknown flags are a usage error.
func parseHostFlags(args []string) ([]string, error) {
	var hosts []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--host" || args[i] == "-host":
			if i+1 >= len(args) {
				return nil, errors.New("--host requires a hostname or IP")
			}
			hosts = append(hosts, args[i+1])
			i++
		case strings.HasPrefix(args[i], "--host="):
			hosts = append(hosts, strings.TrimPrefix(args[i], "--host="))
		default:
			return nil, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	var out []string
	for _, host := range hosts {
		for _, part := range strings.Split(host, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out, nil
}

// caUsage prints the `gotham ca` help.
func caUsage(w io.Writer) {
	fmt.Fprintf(w, `gotham ca <command>

Commands:
  init [--host <name-or-ip>]...  Create the gRPC mTLS certificate authority under
                                 GOTHAM_CA_DIR and persist the optional listener
                                 SAN hosts (repeatable; comma-separated allowed)

GOTHAM_CA_DIR (default ./data/ca/) holds ca.crt (public), ca.key (private, 0600)
and the optional hosts list. The installer runs this once; gotham serve loads it
to run the gRPC gateway over TLS, and GOTHAM_GRPC_HOSTS overrides the host list.
`)
}
