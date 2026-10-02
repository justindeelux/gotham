package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// defaultManagedVolumeRoot is the parent directory of every application's bind
// mounts. A bind source is confined to <root>/<appID>, so a team member can
// never mount the host filesystem (or the Docker socket) into their container.
const defaultManagedVolumeRoot = "/var/lib/gotham/volumes"

// envManagedVolumeRoot overrides defaultManagedVolumeRoot. It must match the
// agent's GOTHAM_AGENT_MANAGED_VOLUME_ROOT (or --managed-volume-root): the
// control plane derives the path and the node re-validates it against its own
// root.
const envManagedVolumeRoot = "GOTHAM_MANAGED_VOLUME_ROOT"

// storageDirInvalid matches anything outside a safe single path segment.
var storageDirInvalid = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// managedVolumeRoot returns the configured managed volume root, cleaned. A
// relative or empty configured value falls back to the default so a typo can
// never silently widen what is allowed.
func managedVolumeRoot() string {
	root := strings.TrimSpace(os.Getenv(envManagedVolumeRoot))
	if root == "" || !strings.HasPrefix(root, "/") {
		return defaultManagedVolumeRoot
	}
	return filepath.Clean(root)
}

// appVolumeDir is the directory every bind of one application must live under.
func appVolumeDir(root string, appID uuid.UUID) string {
	return filepath.Join(filepath.Clean(root), appID.String())
}

// managedHostPath resolves the host source of one storage row:
//
//   - an empty host path is a managed bind: <root>/<appID>/<name>;
//   - a non-absolute host path is a Docker named volume and is passed through
//     unchanged (named volumes are not host binds and need no confinement);
//   - an absolute host path is a bind and must resolve inside <root>/<appID>.
func managedHostPath(root string, appID uuid.UUID, name, host string) (string, error) {
	if host == "" {
		if appID == uuid.Nil {
			return "", fmt.Errorf("%w: storage %q needs an application id for a managed path", ErrValidation, name)
		}
		return filepath.Join(appVolumeDir(root, appID), storageDirName(name)), nil
	}
	if err := validateHostBind(root, appID, name, host); err != nil {
		return "", err
	}
	if !strings.HasPrefix(host, "/") {
		return host, nil
	}
	return filepath.Clean(host), nil
}

// validateHostBind rejects a host bind that escapes the application's managed
// directory or names a known-dangerous host location. A non-absolute host (a
// named volume) and an empty host (a derived managed path) are accepted: they
// are not host binds.
func validateHostBind(root string, appID uuid.UUID, name, host string) error {
	if host == "" || !strings.HasPrefix(host, "/") {
		return nil
	}
	if isDangerousHostPath(host) {
		return fmt.Errorf("%w: storage %q host path %q is not allowed", ErrValidation, name, host)
	}
	if appID == uuid.Nil {
		return fmt.Errorf("%w: storage %q host path %q has no owning application", ErrValidation, name, host)
	}
	base := appVolumeDir(root, appID)
	cleaned := filepath.Clean(host)
	if !withinPath(base, cleaned) {
		return fmt.Errorf("%w: storage %q host path %q must be inside %s", ErrValidation, name, host, base)
	}
	// A symlink already living inside the app directory could point elsewhere;
	// Docker resolves it at mount time. Re-check the resolved target when the
	// path exists (a not-yet-created path cannot be a symlink).
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil && !withinPath(base, resolved) {
		return fmt.Errorf("%w: storage %q host path %q resolves outside %s", ErrValidation, name, host, base)
	}
	return nil
}

// isDangerousHostPath reports whether host names a host location that must
// never be mounted into an application container: the root filesystem, the
// system trees, and the Docker socket (full node control).
func isDangerousHostPath(host string) bool {
	cleaned := filepath.Clean(host)
	switch cleaned {
	case "/", "/etc", "/proc", "/sys", "/dev", "/run", "/boot",
		"/var/run/docker.sock", "/run/docker.sock":
		return true
	}
	return filepath.Base(cleaned) == "docker.sock"
}

// withinPath reports whether path is base or a descendant of base.
func withinPath(base, path string) bool {
	if path == base {
		return true
	}
	return strings.HasPrefix(path, base+string(os.PathSeparator))
}

// storageDirName turns a storage name into a safe single path segment for the
// derived managed path. Names are unique per application, so collisions are
// only possible after sanitization (e.g. "a b" and "a-b").
func storageDirName(name string) string {
	cleaned := strings.Trim(storageDirInvalid.ReplaceAllString(strings.TrimSpace(name), "-"), ".-")
	if cleaned == "" {
		return "volume"
	}
	return cleaned
}
