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

const defaultManagedVolumeRoot = "/var/lib/gotham/volumes"

const envManagedVolumeRoot = "GOTHAM_MANAGED_VOLUME_ROOT"

const appNamedVolumePrefix = "gotham-app-"

var storageDirInvalid = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func managedVolumeRoot() string {
	root := strings.TrimSpace(os.Getenv(envManagedVolumeRoot))
	if root == "" || !strings.HasPrefix(root, "/") {
		return defaultManagedVolumeRoot
	}
	return filepath.Clean(root)
}

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

func appVolumeDir(root string, appID uuid.UUID) string {
	return filepath.Join(filepath.Clean(root), appID.String())
}

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
		prefix := appNamedVolumePrefix + appID.String() + "-"
		if strings.HasPrefix(host, prefix) {
			suffix := strings.TrimPrefix(host, prefix)
			if suffix != "" && suffix == storageDirName(suffix) {
				return host, nil
			}
			return "", fmt.Errorf("%w: storage %q named volume %q is malformed", ErrValidation, name, host)
		}
		return prefix + storageDirName(host), nil
	}
	if err := validateHostBind(root, appID, name, host); err != nil {
		return "", err
	}
	return filepath.Clean(host), nil
}

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
	if filepath.Dir(cleaned) != base {
		return fmt.Errorf("%w: storage %q host path %q must be a direct child of %s", ErrValidation, name, host, base)
	}
	if err := rejectSymlinkComponents(base, cleaned); err != nil {
		return fmt.Errorf("%w: storage %q host path %q: %v", ErrValidation, name, host, err)
	}
	return nil
}

var errMissing = errors.New("missing")

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

func isDangerousHostPath(host string) bool {
	cleaned := filepath.Clean(host)
	switch cleaned {
	case "/", "/etc", "/proc", "/sys", "/dev", "/run", "/boot",
		"/var/run/docker.sock", "/run/docker.sock":
		return true
	}
	return filepath.Base(cleaned) == "docker.sock"
}

func storageDirName(name string) string {
	cleaned := strings.Trim(storageDirInvalid.ReplaceAllString(strings.TrimSpace(name), "-"), ".-")
	if cleaned == "" {
		return "volume"
	}
	return cleaned
}

func warnLegacyStorageResolution(logger *slog.Logger, appID uuid.UUID, storages []Storage) {
	if logger == nil {
		return
	}
	root := managedVolumeRoot()
	for _, storage := range storages {
		host := strings.TrimSpace(storage.HostPath)
		if host == "" || strings.HasPrefix(host, "/") {
			continue
		}
		resolved, err := managedHostPath(root, appID, storage.Name, host)
		if err != nil || resolved == host {
			continue
		}
		logger.Warn("deploy: legacy storage named volume re-namespaced at deploy; the old volume's data is not mounted",
			"application_id", appID, "storage", storage.Name,
			"previous_volume", host, "volume", resolved)
	}
}
