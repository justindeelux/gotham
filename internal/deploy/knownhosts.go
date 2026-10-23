package deploy

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

//go:embed known_hosts
var defaultKnownHosts string

const (
	knownHostsEnv           = "GOTHAM_KNOWN_HOSTS"
	devAcceptNewHostKeysEnv = "GOTHAM_DEV_ACCEPT_NEW_HOST_KEYS"
)

func knownHostsFile() string {
	return strings.TrimSpace(os.Getenv(knownHostsEnv))
}

func devAcceptNewHostKeys() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(devAcceptNewHostKeysEnv)), "true")
}

func strictHostKeyChecking(acceptNew bool) string {
	if acceptNew {
		return "accept-new"
	}
	return "yes"
}

func materializeKnownHosts(dir string, logger *slog.Logger) (string, error) {
	content := defaultKnownHosts
	if extra := knownHostsFile(); extra != "" {
		if info, statErr := os.Stat(extra); statErr == nil && info.Mode().Perm()&0o022 != 0 {
			logger.Warn("deploy: operator known_hosts is group/world-writable",
				"path", extra,
				"mode", fmt.Sprintf("%04o", info.Mode().Perm()),
				"risk", "any local user can replace the pinned host keys; chmod 600",
			)
		}
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
