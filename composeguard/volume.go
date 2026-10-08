package composeguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serviceVolumes validates one service's mounts: anonymous volumes,
// project-local named volumes, confined host-path binds and tmpfs. The
// source classification follows compose: a source whose first character is
// `.`, `/` or `~` is a host path, anything else a named volume.
func (v *validator) serviceVolumes(path string, raw any, declared map[string]bool) error {
	if raw == nil {
		return nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return at(path, "volumes must be a list")
	}
	for _, entry := range entries {
		if err := v.mount(path, entry, declared); err != nil {
			return err
		}
	}
	return nil
}

// mount validates one volume entry in short or long form.
func (v *validator) mount(path string, entry any, declared map[string]bool) error {
	switch mount := entry.(type) {
	case string:
		return v.shortMount(path, mount, declared)
	case map[string]any:
		return v.longMount(path, mount, declared)
	default:
		return at(path, "volume entries must be strings or mappings")
	}
}

// shortMount parses compose's "source:target[:mode]" volume form. A single
// part is an anonymous volume; otherwise the source's first character
// decides: `.`, `/` and `~` start a host path, anything else a named volume.
// A single-letter source is a Windows drive (`C:/...`), which compose-go
// parses as a drive letter while the Linux daemon rejects: it fails closed
// here rather than depending on the daemon.
func (v *validator) shortMount(path, entry string, declared map[string]bool) error {
	parts := strings.Split(entry, ":")
	switch len(parts) {
	case 1:
		target := strings.TrimSpace(parts[0])
		if target == "" {
			return at(path, "volume mount has no target")
		}
		if !strings.HasPrefix(target, "/") {
			return at(path, "volume target %q must be an absolute container path", parts[0])
		}
		return nil // Anonymous volume: no host source to confine.
	case 2, 3:
		source := parts[0]
		if isWindowsDrive(source) {
			return at(path, "volume source %q is not supported", source)
		}
		target := strings.TrimSpace(parts[1])
		if target == "" {
			return at(path, "volume mount has no target")
		}
		if !strings.HasPrefix(target, "/") {
			return at(path, "volume target %q must be an absolute container path", parts[1])
		}
		if len(parts) == 3 {
			if err := checkMountMode(path, parts[2]); err != nil {
				return err
			}
		}
		if isBindSource(source) {
			return v.bindSource(path, source)
		}
		return v.namedSource(path, source, declared)
	default:
		return at(path, "volume %q has too many segments", entry)
	}
}

// isWindowsDrive reports a single-letter source: compose-go reads `C:` as a
// Windows drive letter, so such a source never names a volume here.
func isWindowsDrive(source string) bool {
	if len(source) != 1 {
		return false
	}
	c := source[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// checkMountMode validates the short-form mode segment: read/write,
// SELinux relabeling and copy flags only. Propagation modes (rshared,
// rslave and friends) would leak mounts between containers and fail
// closed here; compose reports anything else at the node.
func checkMountMode(path, mode string) error {
	for _, flag := range strings.Split(mode, ",") {
		switch strings.TrimSpace(flag) {
		case "ro", "rw", "z", "Z", "nocopy":
		case "":
			return at(path, "volume mode %q has an empty flag", mode)
		default:
			return at(path, "volume mode %q is not allowed", mode)
		}
	}
	return nil
}

// isBindSource reports whether a short-form source is a host path, the way
// compose classifies it: any source starting with `.`, `/` or `~`. This
// closes the `..`, `.hidden` and `~` escapes the first-character check used
// to miss.
func isBindSource(source string) bool {
	return strings.HasPrefix(source, "/") ||
		strings.HasPrefix(source, ".") ||
		strings.HasPrefix(source, "~")
}

// longMount validates the long volume syntax. Only bind, volume and tmpfs
// mounts exist for a confined project; every key outside the per-type set
// fails closed.
func (v *validator) longMount(path string, mount map[string]any, declared map[string]bool) error {
	for key := range mount {
		switch key {
		case "type", "source", "target", "read_only", "bind", "volume", "tmpfs", "consistency":
		default:
			return at(path, "volume key %q is not allowed", key)
		}
	}
	target := strings.TrimSpace(scalarString(mount["target"]))
	mountType := strings.ToLower(strings.TrimSpace(scalarString(mount["type"])))
	source := scalarString(mount["source"])
	if err := optionalBool(mount["read_only"]); err != nil {
		return at(path, "read_only must be a boolean")
	}
	switch mountType {
	case "", "volume":
		if source == "" {
			if !strings.HasPrefix(target, "/") {
				return at(path, "volume mount has no target")
			}
			return nil // Anonymous volume.
		}
		if isBindSource(source) {
			return at(path, "volume source %q must be a named volume for type volume", source)
		}
		if !strings.HasPrefix(target, "/") {
			return at(path, "volume target %q must be an absolute container path", target)
		}
		if mount["volume"] != nil {
			if err := checkVolumeOpts(path, mount["volume"]); err != nil {
				return err
			}
		}
		return v.namedSource(path, source, declared)
	case "bind":
		if !strings.HasPrefix(target, "/") {
			return at(path, "volume mount has no target")
		}
		if !isBindSource(source) {
			return at(path, "bind source %q must be a host path", source)
		}
		if bind, ok := mount["bind"].(map[string]any); ok {
			for key, flag := range bind {
				switch key {
				case "create_host_path":
					if err := optionalBool(flag); err != nil {
						return at(path, "bind.create_host_path must be a boolean")
					}
				default:
					return at(path, "bind key %q is not allowed", key)
				}
			}
		} else if mount["bind"] != nil {
			return at(path, "bind must be a mapping")
		}
		return v.bindSource(path, source)
	case "tmpfs":
		if !strings.HasPrefix(target, "/") {
			return at(path, "volume mount has no target")
		}
		if strings.TrimSpace(source) != "" {
			return at(path, "tmpfs mount must not have a source")
		}
		if tmpfs, ok := mount["tmpfs"].(map[string]any); ok {
			for key, limit := range tmpfs {
				switch key {
				case "size", "mode":
					if strings.TrimSpace(scalarString(limit)) == "" {
						return at(path, "tmpfs.%s must not be empty", key)
					}
				default:
					return at(path, "tmpfs key %q is not allowed", key)
				}
			}
		} else if mount["tmpfs"] != nil {
			return at(path, "tmpfs must be a mapping")
		}
		return nil
	default:
		return at(path, "volume type %q is not allowed", mountType)
	}
}

// optionalBool accepts an absent value or a boolean.
func optionalBool(raw any) error {
	switch raw.(type) {
	case nil, bool:
		return nil
	default:
		return fmt.Errorf("not a boolean")
	}
}

// namedSource validates a named volume reference: declared top-level and
// outside the managed-database namespace.
func (v *validator) namedSource(path, source string, declared map[string]bool) error {
	if isWindowsDrive(source) {
		return at(path, "volume source %q is not supported", source)
	}
	if !namedVolumePattern.MatchString(source) {
		return at(path, "volume source %q is not a valid named volume", source)
	}
	if err := v.reservedVolume(source); err != nil {
		return at(path, "%v", err)
	}
	if !declared[source] {
		return at(path, "volume %q has no top-level declaration", source)
	}
	return nil
}

// bindSource confines a host-path bind to the application's managed
// directory: it must be an absolute path (relative sources resolve into the
// agent's state directory, outside the managed volumes), a direct child of
// the managed directory, never a known-dangerous host location, and free of
// symlinks where the filesystem can be inspected.
func (v *validator) bindSource(path, source string) error {
	if !strings.HasPrefix(source, "/") {
		return at(path, "host path %q must be absolute: relative paths land outside the managed volumes", source)
	}
	cleaned := filepath.Clean(source)
	if isDangerousHostPath(cleaned) {
		return at(path, "host path %q is not allowed", source)
	}
	base := v.managedDir()
	if filepath.Dir(cleaned) != base {
		return at(path, "host path %q must be a direct child of %s", source, base)
	}
	if err := checkNoSymlinks(base, cleaned); err != nil {
		return at(path, "host path %q: %v", source, err)
	}
	return nil
}

// isDangerousHostPath reports whether host names a host location that must
// never be mounted into a confined container: the root filesystem, the
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

// checkNoSymlinks Lstat-walks base and every component of path below it,
// rejecting any symlink. The walk starts at base (not "/") so a legitimate
// system symlink such as macOS's /var -> /private/var never affects an
// operator-configured root. A missing component is accepted (compose
// creates it as a real directory); any other Lstat error fails closed.
// Where the filesystem cannot be inspected at all (the control plane
// checking node paths), every component is missing and the lexical checks
// above are what confine the bind.
func checkNoSymlinks(base, path string) error {
	current := filepath.Clean(base)
	if err := checkNotSymlink(current); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	rel, err := filepath.Rel(current, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s escapes %s", path, base)
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := checkNotSymlink(current); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

// checkNotSymlink reports a symlink error for a symlink and any other Lstat
// failure unchanged (fail closed).
func checkNotSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", path)
	}
	return nil
}

// checkVolumeOpts validates the volume options of a named-volume mount:
// nocopy only, plus a subpath confined inside the volume. A subpath
// escape (`..` or absolute) would break out of the volume on the node.
func checkVolumeOpts(path string, raw any) error {
	opts, ok := raw.(map[string]any)
	if !ok {
		return at(path, "volume options must be a mapping")
	}
	for key, value := range opts {
		switch key {
		case "nocopy":
			if err := optionalBool(value); err != nil {
				return at(path, "volume.nocopy must be a boolean")
			}
		case "subpath":
			subpath, ok := value.(string)
			if !ok {
				return at(path, "volume.subpath must be a string")
			}
			cleaned := filepath.Clean(strings.TrimSpace(subpath))
			if filepath.IsAbs(cleaned) || cleaned == ".." ||
				strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
				return at(path, "volume.subpath %q escapes the volume", subpath)
			}
			for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
				if component == ".." {
					return at(path, "volume.subpath %q escapes the volume", subpath)
				}
			}
		default:
			return at(path, "volume key %q is not allowed", key)
		}
	}
	return nil
}
