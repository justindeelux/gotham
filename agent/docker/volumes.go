package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent/proxy"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

var ErrInvalidVolumeBind = errors.New("docker: invalid volume bind")

const (
	labelAppID     = "gotham.app_id"
	labelComponent = "gotham.component"
	componentProxy = "proxy"
)

const appNamedVolumePrefix = "gotham-app-"

func validateContainerVolumes(root, proxyRoot string, req *agentv1.CreateContainerRequest) error {
	volumes := req.GetVolumes()
	if len(volumes) == 0 {
		return nil
	}
	labels := req.GetLabels()
	component := strings.TrimSpace(labels[labelComponent])
	appID := strings.TrimSpace(labels[labelAppID])
	for _, spec := range volumes {
		host := bindSource(spec)
		if host == "" {
			return fmt.Errorf("%w: %q has no source", ErrInvalidVolumeBind, spec)
		}
		switch {
		case component == componentProxy:
			if !isManagedProxy(labels, req.GetName()) {
				return fmt.Errorf("%w: component=proxy requires the managed proxy container identity", ErrInvalidVolumeBind)
			}
			if err := validateProxyBind(proxyRoot, host); err != nil {
				return err
			}
		case appID != "":
			if err := validateAppVolume(root, appID, host); err != nil {
				return err
			}
		default:
			if strings.HasPrefix(host, "/") {
				return fmt.Errorf("%w: host path %q is not allowed on a non-application container", ErrInvalidVolumeBind, host)
			}
		}
	}
	return nil
}

func validateAppVolume(root, appID, host string) error {
	if !strings.HasPrefix(host, "/") {
		id, err := parseAppID(appID)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(host, appNamedVolumePrefix+id.String()+"-") {
			return fmt.Errorf("%w: named volume %q is not scoped to application %s", ErrInvalidVolumeBind, host, id)
		}
		return nil
	}
	if isDangerousHostPath(host) {
		return fmt.Errorf("%w: host path %q is not allowed", ErrInvalidVolumeBind, host)
	}
	id, err := parseAppID(appID)
	if err != nil {
		return err
	}
	base := filepath.Join(filepath.Clean(root), id.String())
	cleaned := filepath.Clean(host)
	if filepath.Dir(cleaned) != base {
		return fmt.Errorf("%w: host path %q must be a direct child of %s", ErrInvalidVolumeBind, host, base)
	}
	if err := rejectSymlinkComponents(base, cleaned); err != nil {
		return fmt.Errorf("%w: host path %q: %v", ErrInvalidVolumeBind, host, err)
	}
	return nil
}

func isManagedProxy(labels map[string]string, name string) bool {
	return strings.TrimSpace(name) == proxy.ContainerName &&
		strings.TrimSpace(labels["gotham.managed"]) == "true"
}

func validateProxyBind(proxyRoot, host string) error {
	if !strings.HasPrefix(host, "/") {
		return nil
	}
	if isDangerousHostPath(host) {
		return fmt.Errorf("%w: host path %q is not allowed", ErrInvalidVolumeBind, host)
	}
	base := filepath.Clean(proxyRoot)
	cleaned := filepath.Clean(host)
	if !withinPath(base, cleaned) {
		return fmt.Errorf("%w: proxy host path %q must be inside %s", ErrInvalidVolumeBind, host, base)
	}
	if err := rejectSymlinkComponents(base, cleaned); err != nil {
		return fmt.Errorf("%w: proxy host path %q: %v", ErrInvalidVolumeBind, host, err)
	}
	return nil
}

func parseAppID(appID string) (uuid.UUID, error) {
	id, err := uuid.Parse(appID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: application label %q is not an id", ErrInvalidVolumeBind, appID)
	}
	return id, nil
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

func bindSource(spec string) string {
	if index := strings.IndexByte(spec, ':'); index >= 0 {
		return strings.TrimSpace(spec[:index])
	}
	return ""
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

func withinPath(base, path string) bool {
	if base == "" || path == "" || !filepath.IsAbs(base) || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
