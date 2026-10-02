package builds

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// defaultMaxContextBytes bounds the uncompressed build context the control
// plane assembles in memory. It matches the agent's received-context limit, so
// an oversized repository fails on the control plane with a clear error
// instead of exhausting its memory before the agent-side check runs.
const defaultMaxContextBytes = int64(512 << 20)

// dockerIgnoreFile is the per-repository ignore file honoured while assembling
// a build context. The Docker daemon does not apply it to a tar context sent
// directly over the Engine API (empirically: a `.env` listed in the
// repository's `.dockerignore` is still copied into the image), so the control
// plane must filter the context itself.
const dockerIgnoreFile = ".dockerignore"

// excludedContextEntries are skipped wherever they appear in a build context.
var excludedContextEntries = map[string]bool{
	".git":      true,
	".DS_Store": true,
	"__MACOSX":  true,
}

// contextSpec describes one build-context assembly.
type contextSpec struct {
	// root is the directory whose contents are packed.
	root string
	// ignoreDir holds the .dockerignore honoured for the context. Empty means
	// root. The static engine points it at the repository root so the
	// repository's ignore rules still apply when the site lives in public/.
	ignoreDir string
	// extra files (path -> contents) are appended after the tree, letting an
	// engine synthesise a Dockerfile next to the sources. Their names are
	// context-relative and are not subject to the repository's ignore rules.
	extra map[string][]byte
	// maxBytes caps the uncompressed context size; <= 0 means the default.
	maxBytes int64
	// keep is a context-relative path that is never ignored, together with its
	// parent directories. It is the repository's Dockerfile: the builder needs
	// it even when the repository's .dockerignore lists it, matching the Docker
	// CLI, which keeps the Dockerfile in the context while excluding it from
	// COPY. Empty means nothing is protected.
	keep string
}

// buildContextTar packs the spec's root into an uncompressed tar build context
// and appends the extra files. The repository's .dockerignore is honoured for
// the tree (the Docker daemon does not apply it to a tar context), symlinks are
// preserved as symlink entries, and the total size is bounded so a huge
// repository cannot exhaust the control plane's memory.
func buildContextTar(spec contextSpec) ([]byte, error) {
	dir := spec.root
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: stat %s: %v", ErrValidation, dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrValidation, dir)
	}

	limit := spec.maxBytes
	if limit <= 0 {
		limit = defaultMaxContextBytes
	}
	ignore, err := loadDockerIgnore(spec.ignoreDir, dir)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	limited := &limitedWriter{writer: &buf, limit: limit}
	tw := tar.NewWriter(limited)

	err = filepath.WalkDir(dir, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if excludedContextEntries[entry.Name()] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := filepath.ToSlash(rel)
		isDir := entry.IsDir()
		// The Dockerfile and its parent directories are never filtered: the
		// builder must be able to read it even when .dockerignore lists it.
		protected := spec.keep != "" &&
			(name == spec.keep || strings.HasPrefix(spec.keep, name+"/"))
		if !protected && ignore.ignored(name, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		// A synthesized extra takes precedence over a repository file of the
		// same name (the static engine's .dockerignore); extras are appended
		// after the walk, so skipping here avoids a duplicate tar entry.
		if _, isExtra := spec.extra[name]; !isDir && isExtra {
			return nil
		}
		if isDir {
			return tw.WriteHeader(&tar.Header{
				Name:     name + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			})
		}
		fileInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		switch {
		case fileInfo.Mode()&os.ModeSymlink != 0:
			return writeSymlinkEntry(tw, name, p, ignore)
		case fileInfo.Mode().IsRegular():
			return writeRegularEntry(tw, p, name, fileInfo)
		default:
			// Sockets, devices and FIFOs are intentionally skipped.
			return nil
		}
	})
	if err != nil {
		return nil, fmt.Errorf("builds: pack context %s: %w", dir, err)
	}

	for _, name := range sortedKeys(spec.extra) {
		content := spec.extra[name]
		if err := tw.WriteHeader(&tar.Header{
			Name:     filepath.ToSlash(name),
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeRegularEntry streams one regular file into the context.
func writeRegularEntry(tw *tar.Writer, fullPath, name string, info fs.FileInfo) error {
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     int64(info.Mode().Perm()),
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(tw, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// writeSymlinkEntry preserves a symlink as a symlink tar entry. The ignore
// rules are applied to the link and to its target: a link whose target resolves
// inside the context to an excluded path (for example `.env`) is dropped, so an
// excluded secret is never reintroduced through a link. Absolute links and
// links escaping the context have no context-relative target to test, so they
// are preserved as-is — Docker (and the builder) treat their targets as
// container paths, not host paths.
func writeSymlinkEntry(tw *tar.Writer, name, fullPath string, ignore dockerIgnore) error {
	target, err := os.Readlink(fullPath)
	if err != nil {
		return err
	}
	if resolved, ok := contextRelativeTarget(name, target); ok && ignore.ignored(resolved, false) {
		return nil
	}
	return tw.WriteHeader(&tar.Header{
		Name:     name,
		Linkname: target,
		Mode:     0o777,
		Typeflag: tar.TypeSymlink,
	})
}

// contextRelativeTarget resolves a symlink's target to a slash-separated path
// relative to the context root. ok is false when the target is absolute or
// escapes the root, in which case no context pattern can be matched against it.
func contextRelativeTarget(linkName, target string) (string, bool) {
	targetSlash := filepath.ToSlash(target)
	if path.IsAbs(targetSlash) {
		return "", false
	}
	resolved := path.Clean(path.Join(path.Dir(linkName), targetSlash))
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", false
	}
	return resolved, true
}

// limitedWriter caps the bytes written to an in-memory buffer, returning an
// ErrValidation-wrapped error as soon as the cap is exceeded so the context
// assembly aborts before the buffer can grow without bound.
type limitedWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

// Write implements io.Writer.
func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.written+int64(len(p)) > w.limit {
		return 0, fmt.Errorf("%w: build context exceeds %d bytes", ErrValidation, w.limit)
	}
	n, err := w.writer.Write(p)
	w.written += int64(n)
	return n, err
}

// dockerIgnore is the compiled form of a repository's .dockerignore.
type dockerIgnore []ignorePattern

// ignorePattern is one non-comment, non-empty .dockerignore line.
type ignorePattern struct {
	pattern string
	negate  bool
	dirOnly bool
}

// loadDockerIgnore reads and compiles the dockerignore for a context. A missing
// file is not an error: there is simply nothing to exclude.
func loadDockerIgnore(ignoreDir, root string) (dockerIgnore, error) {
	if strings.TrimSpace(ignoreDir) == "" {
		ignoreDir = root
	}
	data, err := os.ReadFile(filepath.Join(ignoreDir, dockerIgnoreFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("builds: read %s: %w", dockerIgnoreFile, err)
	}
	return parseDockerIgnore(string(data)), nil
}

// parseDockerIgnore compiles .dockerignore lines. Blank lines and comments are
// skipped; a leading `!` negates (re-includes); a trailing `/` restricts the
// pattern to directories; a leading `/` is normalised away. Pattern syntax is
// Docker's subset of shell globs (see matchContextPattern).
func parseDockerIgnore(content string) dockerIgnore {
	var patterns dockerIgnore
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed := ignorePattern{}
		if strings.HasPrefix(line, "!") {
			parsed.negate = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
			if line == "" {
				continue
			}
		}
		if strings.HasSuffix(line, "/") {
			parsed.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		line = strings.TrimPrefix(line, "./")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		parsed.pattern = line
		patterns = append(patterns, parsed)
	}
	return patterns
}

// ignored reports whether rel (slash-separated, relative to the context root)
// is excluded. The last matching pattern wins and a negated pattern
// re-includes; an excluded directory is skipped entirely, matching Docker (a
// negated child cannot be re-included once its parent is excluded).
func (d dockerIgnore) ignored(rel string, isDir bool) bool {
	excluded := false
	for _, p := range d {
		if p.dirOnly && !isDir {
			continue
		}
		if !matchContextPattern(p.pattern, rel) {
			continue
		}
		excluded = !p.negate
	}
	return excluded
}

// matchContextPattern matches a Docker ignore pattern against a
// context-relative path. `*` and `?` match within one path segment and `**`
// matches any number of segments (including none), which covers Docker's
// documented syntax. Escapes and `[`-classes beyond path.Match are not
// supported; a pattern that path.Match cannot parse simply does not match.
func matchContextPattern(pattern, rel string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

// matchSegments matches pattern segments against name segments, treating a `**`
// segment as a wildcard for zero or more name segments.
func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], name[1:])
}
