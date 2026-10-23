package composeguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (v *validator) serviceVolumes(path string, raw any) error {
	if raw == nil {
		return nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return at(path, "volumes must be a list")
	}
	for _, entry := range entries {
		if err := v.mount(path, entry); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) mount(path string, entry any) error {
	switch mount := entry.(type) {
	case string:
		return v.shortMount(path, mount)
	case map[string]any:
		return v.longMount(path, mount)
	default:
		return at(path, "volume entries must be strings or mappings")
	}
}

func (v *validator) shortMount(path, entry string) error {
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
		return nil
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
		return v.namedSource(path, source)
	default:
		return at(path, "volume %q has too many segments", entry)
	}
}

func isWindowsDrive(source string) bool {
	if len(source) != 1 {
		return false
	}
	c := source[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

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

func isBindSource(source string) bool {
	return strings.HasPrefix(source, "/") ||
		strings.HasPrefix(source, ".") ||
		strings.HasPrefix(source, "~")
}

func (v *validator) longMount(path string, mount map[string]any) error {
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
			return nil
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
		return v.namedSource(path, source)
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

func optionalBool(raw any) error {
	switch raw.(type) {
	case nil, bool:
		return nil
	default:
		return fmt.Errorf("not a boolean")
	}
}

func (v *validator) namedSource(path, source string) error {
	if isWindowsDrive(source) {
		return at(path, "volume source %q is not supported", source)
	}
	if !namedVolumePattern.MatchString(source) {
		return at(path, "volume source %q is not a valid named volume", source)
	}
	if err := v.reservedVolume(source); err != nil {
		return at(path, "%v", err)
	}
	return nil
}

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

func isDangerousHostPath(host string) bool {
	cleaned := filepath.Clean(host)
	switch cleaned {
	case "/", "/etc", "/proc", "/sys", "/dev", "/run", "/boot",
		"/var/run/docker.sock", "/run/docker.sock":
		return true
	}
	return filepath.Base(cleaned) == "docker.sock"
}

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
