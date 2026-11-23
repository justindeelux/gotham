package deploy

import (
	"fmt"
	"path"
	"strings"
)

const maxWorkingDirLen = 255

// normalizeWorkingDir bounds the app's build folder to a relative path inside
// the clone. Empty stays empty (the repository root), so every app created
// before GOTHAM-268 keeps building from the root.
func normalizeWorkingDir(raw string) (string, error) {
	dir := strings.TrimSpace(raw)
	if dir == "" {
		return "", nil
	}
	if len(dir) > maxWorkingDirLen {
		return "", fmt.Errorf("%w: working directory exceeds %d bytes", ErrValidation, maxWorkingDirLen)
	}
	if strings.HasPrefix(dir, "/") || path.IsAbs(dir) || strings.Contains(dir, `\`) {
		return "", fmt.Errorf("%w: working directory %q must be a relative path inside the repository", ErrValidation, dir)
	}
	for _, segment := range strings.Split(dir, "/") {
		switch segment {
		case "", ".":
			return "", fmt.Errorf("%w: working directory %q must be a plain relative path inside the repository", ErrValidation, dir)
		case "..":
			return "", fmt.Errorf("%w: working directory %q must not leave the repository", ErrValidation, dir)
		case ".git":
			return "", fmt.Errorf("%w: working directory %q must not point into .git", ErrValidation, dir)
		}
	}
	return dir, nil
}

// repoBacked reports whether the app builds from a cloned repository, the only
// sources a working directory applies to.
func repoBacked(app Application) bool {
	switch NormalizeSourceType(app.SourceType, app.Provider) {
	case SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp, SourceCompose:
		return strings.TrimSpace(app.CloneURL) != ""
	default:
		return false
	}
}
