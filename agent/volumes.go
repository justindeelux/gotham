package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// ErrInvalidVolumeBind marks a volume bind the node refuses: an absolute host
// source outside the allowed location, a known-dangerous source such as the
// Docker socket, a named volume not scoped to its application, or a bind with
// no owning application. The DockerService error mapper turns it into
// InvalidArgument.
var ErrInvalidVolumeBind = errors.New("docker: invalid volume bind")

// Control-plane labels the node classifies a container by. The node uses them
// to confine binds independently of the control plane (defense in depth
// against a compromised or stale control plane).
const (
	// labelAppID is the control plane's application label (deploy.labelAppID).
	labelAppID = "gotham.app_id"
	// labelComponent marks a managed container's role. The proxy mounts the
	// node's own Traefik directory, which is not an application bind.
	labelComponent = "gotham.component"
	// componentProxy is the managed proxy container.
	componentProxy = "proxy"
)

// appNamedVolumePrefix namespaces every application named volume
// (deploy.appNamedVolumePrefix): a bare name can never alias another
// application's volume or a managed database volume (gotham-db-*).
const appNamedVolumePrefix = "gotham-app-"

// validateContainerVolumes enforces the managed-volume rule on a container
// request before it reaches Docker, per component:
//
//   - a proxy container may mount the node's own proxy directory;
//   - an application container may mount binds under <managed root>/<app id>
//     and named volumes namespaced to that application;
//   - every other managed container (database, backup job) may use named
//     volumes but no host bind.
//
// A bind that cannot be attributed is refused; validation is never skipped.
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
			// A managed database or backup container: named volumes only. An
			// absolute bind on such a container is never legitimate.
			if strings.HasPrefix(host, "/") {
				return fmt.Errorf("%w: host path %q is not allowed on a non-application container", ErrInvalidVolumeBind, host)
			}
		}
	}
	return nil
}

// validateAppVolume checks one mount of an application container: an absolute
// host bind must be a direct child of <root>/<app id>; a named volume must be
// namespaced to the same application.
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

// isManagedProxy reports whether a request carries the node's managed proxy
// identity: the fixed proxy container name and the gotham.managed label. This
// keeps a merely relabelled container from reaching the proxy directory (and
// its ACME private keys). A compromised control plane can still spoof both, so
// this raises the bar rather than making the proxy branch tamper-proof.
func isManagedProxy(labels map[string]string, name string) bool {
	return strings.TrimSpace(name) == defaultTraefikContainerName &&
		strings.TrimSpace(labels["gotham.managed"]) == "true"
}

// validateProxyBind checks a proxy container mount: it must live inside the
// node's own proxy directory (the configuration and ACME directories) and no
// component may be a symlink. The proxy uses no named volumes.
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

// parseAppID validates the application label as a canonical UUID, so a
// manipulated label cannot redirect the confinement root.
func parseAppID(appID string) (uuid.UUID, error) {
	id, err := uuid.Parse(appID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: application label %q is not an id", ErrInvalidVolumeBind, appID)
	}
	return id, nil
}

// errMissing signals that a path component does not exist yet; everything at
// or below it is therefore absent and safe to create.
var errMissing = errors.New("missing")

// rejectSymlinkComponents Lstat-walks base and every component of path below
// it, rejecting any symlink. The walk starts at base (not "/") so a legitimate
// system symlink such as macOS's /var → /private/var never affects the
// operator-configured root, while a symlink a container could plant inside its
// managed directory is still caught. A missing component is accepted; any
// other Lstat error fails closed.
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

// bindSource returns the host side of a "source:target" (or
// "source:target:mode") mount spec. A spec without a colon has no source.
func bindSource(spec string) string {
	if index := strings.IndexByte(spec, ':'); index >= 0 {
		return strings.TrimSpace(spec[:index])
	}
	return ""
}

// isDangerousHostPath reports whether host names a location that must never be
// mounted into an application container.
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
