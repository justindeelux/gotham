package deploy

import (
	"errors"
	"fmt"
	"log/slog"
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

// appNamedVolumePrefix namespaces every Docker named volume an application
// uses, so two applications (or two teams) can never share a bare name and an
// application can never mount a managed database volume (gotham-db-*) or
// another reserved volume.
const appNamedVolumePrefix = "gotham-app-"

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

// warnManagedVolumeRoot logs one startup warning when the root is not
// configured, so an operator running without GOTHAM_MANAGED_VOLUME_ROOT knows
// which implicit default the control plane and node must agree on.
func warnManagedVolumeRoot(logger *slog.Logger) {
	configured := strings.TrimSpace(os.Getenv(envManagedVolumeRoot))
	switch {
	case configured == "":
		logger.Warn("deploy: GOTHAM_MANAGED_VOLUME_ROOT is unset; using the default managed volume root",
			"root", defaultManagedVolumeRoot)
	case !strings.HasPrefix(configured, "/"):
		logger.Warn("deploy: GOTHAM_MANAGED_VOLUME_ROOT must be absolute; using the default managed volume root",
			"configured", configured, "root", defaultManagedVolumeRoot)
	}
}

// appVolumeDir is the directory every bind of one application must live under.
func appVolumeDir(root string, appID uuid.UUID) string {
	return filepath.Join(filepath.Clean(root), appID.String())
}

// managedHostPath resolves the host source of one storage row:
//
//   - an empty host path is a managed bind: <root>/<appID>/<name>;
//   - a non-absolute host path is a Docker named volume, namespaced per
//     application as gotham-app-<appID>-<name>;
//   - an absolute host path is a bind and must be a direct child of
//     <root>/<appID>.
func managedHostPath(root string, appID uuid.UUID, name, host string) (string, error) {
	if host == "" {
		if appID == uuid.Nil {
			return "", fmt.Errorf("%w: storage %q needs an application id for a managed path", ErrValidation, name)
		}
		return filepath.Join(appVolumeDir(root, appID), storageDirName(name)), nil
	}
	if !strings.HasPrefix(host, "/") {
		if appID == uuid.Nil {
			return "", fmt.Errorf("%w: storage %q named volume needs an application id", ErrValidation, name)
		}
		// Idempotent: a name already carrying this application's namespace is
		// returned unchanged, so normalizing an already-normalized row (a
		// GET→PUT round-trip) cannot double the prefix.
		if strings.HasPrefix(host, appNamedVolumePrefix+appID.String()+"-") {
			return host, nil
		}
		return appNamedVolumePrefix + appID.String() + "-" + storageDirName(host), nil
	}
	if err := validateHostBind(root, appID, name, host); err != nil {
		return "", err
	}
	return filepath.Clean(host), nil
}

// validateHostBind rejects a host bind that escapes the application's managed
// directory or names a known-dangerous host location. A non-absolute host (a
// named volume) and an empty host (a derived managed path) are accepted: they
// are not host binds. An explicit bind must be a DIRECT child of
// <root>/<appID> and no path component may be a symlink.
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
	// A direct child only: the app directory itself and any nested path are
	// rejected here (filepath.Dir(base) != base).
	if filepath.Dir(cleaned) != base {
		return fmt.Errorf("%w: storage %q host path %q must be a direct child of %s", ErrValidation, name, host, base)
	}
	if err := rejectSymlinkComponents(base, cleaned); err != nil {
		return fmt.Errorf("%w: storage %q host path %q: %v", ErrValidation, name, host, err)
	}
	return nil
}

// errMissing signals that a path component does not exist yet; everything at
// or below it is therefore absent and safe to create.
var errMissing = errors.New("missing")

// rejectSymlinkComponents Lstat-walks base and every component of path below
// it, rejecting any symlink. The walk starts at base (not "/") so a legitimate
// system symlink such as macOS's /var → /private/var never affects an
// operator-configured root, while a symlink the container itself could plant
// inside its managed directory is still caught. A missing component is
// accepted (Docker creates it as a real directory); any other Lstat error
// fails closed.
func rejectSymlinkComponents(base, path string) error {
	current := filepath.Clean(base)
	if err := checkNotSymlink(current); err != nil {
		if errors.Is(err, errMissing) {
			return nil
		}
		return err
	}
	rel, err := filepath.Rel(current, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%s escapes %s", path, base)
	}
	for _, component := range strings.Split(rel, string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := checkNotSymlink(current); err != nil {
			if errors.Is(err, errMissing) {
				return nil
			}
			return err
		}
	}
	return nil
}

// checkNotSymlink reports errMissing for a nonexistent path, a symlink error
// for a symlink, and any other Lstat failure unchanged (fail closed).
func checkNotSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errMissing
		}
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
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

// storageDirName turns a storage (or named-volume) name into a safe single
// path segment. Sanitized names may collide (e.g. "a b" and "a-b"); callers
// reject the collision rather than sharing one directory.
func storageDirName(name string) string {
	cleaned := strings.Trim(storageDirInvalid.ReplaceAllString(strings.TrimSpace(name), "-"), ".-")
	if cleaned == "" {
		return "volume"
	}
	return cleaned
}
