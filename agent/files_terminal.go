package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/justindeelux/gotham/agent/fsroot"
)

// ResolveShellDir resolves a FileService root-relative directory to the
// absolute host path a terminal's initial working directory may use.
//
// It applies the same policy as the file manager: the root is one of the
// agent's allow-listed roots, the relative path must stay inside it (an
// absolute path or a ".." segment is refused), and a symbolic link whose
// target leaves the root is refused rather than followed. The result must be
// an existing directory. The path is resolved (symlinks evaluated) before it is
// handed to the child process, and the caller sets it directly on the process —
// it is never interpolated into a shell command.
//
// This confines only the *starting directory*. A host shell can cd anywhere the
// agent's service user may read; see the TerminalService contract in
// proto/agent/v1/agent.proto.
func (s *FileServer) ResolveShellDir(rootName, rel string) (string, error) {
	root, err := s.root(rootName)
	if err != nil {
		return "", err
	}
	cleaned, err := fsroot.Clean(rel)
	if err != nil {
		return "", fmt.Errorf("%w: %v", fsroot.ErrInvalid, err)
	}
	base, err := filepath.EvalSymlinks(root.Path())
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", rootName, err)
	}
	candidate := filepath.Join(base, filepath.FromSlash(cleaned))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", fsroot.ErrNotFound, cleaned)
		}
		if errors.Is(err, os.ErrPermission) {
			return "", fmt.Errorf("%w: %s", fsroot.ErrPermission, cleaned)
		}
		return "", fmt.Errorf("resolve %q: %w", cleaned, err)
	}
	if !withinPath(base, resolved) {
		return "", fmt.Errorf("%w: %q resolves outside root %q", fsroot.ErrEscape, rel, rootName)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %s", fsroot.ErrNotFound, cleaned)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %q is not a directory", fsroot.ErrInvalid, cleaned)
	}
	return resolved, nil
}
