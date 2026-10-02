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
// source outside the application's managed directory, a known-dangerous source
// such as the Docker socket, or a bind with no owning application. The
// DockerService error mapper turns it into InvalidArgument.
var ErrInvalidVolumeBind = errors.New("docker: invalid volume bind")

// labelAppID is the control plane's application label. The node uses it to
// confine a bind to <managed root>/<app id>, independently of the control
// plane (defense in depth against a compromised or stale control plane).
const labelAppID = "gotham.app_id"

// validateContainerVolumes enforces the managed-volume rule on a container
// request before it reaches Docker. A non-absolute source is a Docker named
// volume and is left alone; every absolute source must resolve inside the
// application's managed directory.
func validateContainerVolumes(root string, req *agentv1.CreateContainerRequest) error {
	volumes := req.GetVolumes()
	if len(volumes) == 0 {
		return nil
	}
	appID := strings.TrimSpace(req.GetLabels()[labelAppID])
	for _, spec := range volumes {
		host := bindSource(spec)
		if host == "" {
			return fmt.Errorf("%w: %q has no source", ErrInvalidVolumeBind, spec)
		}
		if !strings.HasPrefix(host, "/") {
			continue // named volume: not a host path
		}
		if err := validateVolumeBind(root, appID, host); err != nil {
			return err
		}
	}
	return nil
}

// validateVolumeBind re-checks one absolute host bind: it must name the
// application's own managed directory and no dangerous host location.
func validateVolumeBind(root, appID, host string) error {
	if isDangerousHostPath(host) {
		return fmt.Errorf("%w: host path %q is not allowed", ErrInvalidVolumeBind, host)
	}
	if appID == "" {
		return fmt.Errorf("%w: host path %q has no application label", ErrInvalidVolumeBind, host)
	}
	id, err := uuid.Parse(appID)
	if err != nil {
		return fmt.Errorf("%w: application label %q is not an id", ErrInvalidVolumeBind, appID)
	}
	base := filepath.Join(filepath.Clean(root), id.String())
	cleaned := filepath.Clean(host)
	if !withinPath(base, cleaned) {
		return fmt.Errorf("%w: host path %q must be inside %s", ErrInvalidVolumeBind, host, base)
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil && !withinPath(base, resolved) {
		return fmt.Errorf("%w: host path %q resolves outside %s", ErrInvalidVolumeBind, host, base)
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
