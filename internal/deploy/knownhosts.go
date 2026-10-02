package deploy

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultKnownHosts is the pinned host-key set for the public Git providers
// (github.com, gitlab.com). It is embedded so every keyed clone verifies the
// provider even when the control plane has no ~/.ssh/known_hosts of its own.
//
//go:embed known_hosts
var defaultKnownHosts string

const (
	// knownHostsEnv points at an additional known_hosts file, merged over the
	// embedded set. Operators add their self-hosted Git host's key here.
	knownHostsEnv = "GOTHAM_KNOWN_HOSTS"
	// devAcceptNewHostKeysEnv re-enables StrictHostKeyChecking=accept-new.
	// Test/dev only: it makes the cloner trust a previously unseen host key,
	// which is exactly the MITM exposure this package closes. Never the default.
	devAcceptNewHostKeysEnv = "GOTHAM_DEV_ACCEPT_NEW_HOST_KEYS"
)

// knownHostsFile returns the operator-configured known_hosts path, or "".
func knownHostsFile() string {
	return strings.TrimSpace(os.Getenv(knownHostsEnv))
}

// devAcceptNewHostKeys reports whether the explicit dev escape hatch is on.
func devAcceptNewHostKeys() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(devAcceptNewHostKeysEnv)), "true")
}

// strictHostKeyChecking returns the StrictHostKeyChecking value: "yes" by
// default, "accept-new" only under the documented dev flag.
func strictHostKeyChecking(acceptNew bool) string {
	if acceptNew {
		return "accept-new"
	}
	return "yes"
}

// materializeKnownHosts writes the pinned set into dir and returns its path.
// The set is the embedded public-provider keys plus, when GOTHAM_KNOWN_HOSTS is
// set, the operator's file appended verbatim. The file lives with the ephemeral
// deploy key and is removed with it.
func materializeKnownHosts(dir string) (string, error) {
	content := defaultKnownHosts
	if extra := knownHostsFile(); extra != "" {
		data, err := os.ReadFile(extra)
		if err != nil {
			return "", fmt.Errorf("deploy: read %s (%s): %w", knownHostsEnv, extra, err)
		}
		content = strings.TrimRight(content, "\n") + "\n" + string(data)
	}
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("deploy: write known_hosts: %w", err)
	}
	return path, nil
}
