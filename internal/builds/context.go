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

// maxDockerIgnoreBytes bounds the .dockerignore read so a repository cannot
// exhaust control-plane memory with an enormous ignore file.
const maxDockerIgnoreBytes = int64(1 << 20)

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
	// repository's ignore rules still apply when the site lives in public/;
	// patterns are then matched against the path relative to ignoreDir (true
	// repository-relative semantics).
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
	// Patterns come from ignoreDir but paths are packed relative to root, so
	// match against the path rebased onto ignoreDir.
	prefix := ignorePrefix(spec.ignoreDir, dir)

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
		if !protected && ignore.ignored(rebased(prefix, name)) {
			// Descend into an excluded directory when a later negation could
			// re-include a child (Docker keeps `docs/keep.md` for
			// `docs` + `!docs/keep.md`).
			if isDir && !ignore.hasNegations {
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
			return writeSymlinkEntry(tw, name, p, ignore, prefix)
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

// ignorePrefix returns the slash-separated path from ignoreDir to root, or ""
// when they are the same (or root is not local to ignoreDir).
func ignorePrefix(ignoreDir, root string) string {
	if strings.TrimSpace(ignoreDir) == "" || ignoreDir == root {
		return ""
	}
	rel, err := filepath.Rel(ignoreDir, root)
	if err != nil || !filepath.IsLocal(rel) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return ""
	}
	return rel
}

// rebased joins the ignore-dir prefix to a context-relative path.
func rebased(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
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
func writeSymlinkEntry(tw *tar.Writer, name, fullPath string, ignore dockerIgnore, prefix string) error {
	target, err := os.Readlink(fullPath)
	if err != nil {
		return err
	}
	if resolved, ok := contextRelativeTarget(name, target); ok && ignore.ignored(rebased(prefix, resolved)) {
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
type dockerIgnore struct {
	patterns []ignorePattern
	// hasNegations reports whether any `!` pattern is present; when it is, the
	// walk must descend into excluded directories so a negation can re-include
	// a child.
	hasNegations bool
}

// ignorePattern is one non-comment, non-empty .dockerignore line.
type ignorePattern struct {
	pattern string
	negate  bool
}

// loadDockerIgnore reads and compiles the dockerignore for a context. A missing
// file is not an error: there is simply nothing to exclude. A symlink is
// followed (the Docker CLI does), but a non-regular target (a device, FIFO or
// directory) is treated as absent, and the read is capped so a huge file cannot
// exhaust memory.
func loadDockerIgnore(ignoreDir, root string) (dockerIgnore, error) {
	if strings.TrimSpace(ignoreDir) == "" {
		ignoreDir = root
	}
	fullPath := filepath.Join(ignoreDir, dockerIgnoreFile)
	info, err := os.Stat(fullPath)
	if errors.Is(err, fs.ErrNotExist) {
		return dockerIgnore{}, nil
	}
	if err != nil {
		return dockerIgnore{}, fmt.Errorf("builds: stat %s: %w", dockerIgnoreFile, err)
	}
	if !info.Mode().IsRegular() {
		return dockerIgnore{}, nil
	}
	file, err := os.Open(fullPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dockerIgnore{}, nil
		}
		return dockerIgnore{}, fmt.Errorf("builds: open %s: %w", dockerIgnoreFile, err)
	}
	defer func() { _ = file.Close() }()
	// Guard against the target becoming non-regular between Stat and Open (for
	// example a symlink swapped to /dev/zero).
	if opened, statErr := file.Stat(); statErr != nil || !opened.Mode().IsRegular() {
		return dockerIgnore{}, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDockerIgnoreBytes+1))
	if err != nil {
		return dockerIgnore{}, fmt.Errorf("builds: read %s: %w", dockerIgnoreFile, err)
	}
	if int64(len(data)) > maxDockerIgnoreBytes {
		return dockerIgnore{}, fmt.Errorf("%w: %s exceeds %d bytes", ErrValidation, dockerIgnoreFile, maxDockerIgnoreBytes)
	}
	return parseDockerIgnore(string(data)), nil
}

// parseDockerIgnore compiles .dockerignore lines. A UTF-8 BOM is stripped
// (the Docker CLI strips it, and without this the first rule would carry the
// BOM and never match). Blank lines and comments are skipped; a leading `!`
// negates (re-includes); the pattern is path.Clean-ed like Docker, so `cache/`,
// `./cache`, `a//b` and `a/./b` normalise, and patterns escaping the root are
// dropped. Pattern syntax is Docker's subset of shell globs (see
// matchContextPattern).
func parseDockerIgnore(content string) dockerIgnore {
	content = strings.TrimPrefix(content, "\ufeff")
	compiled := dockerIgnore{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed := ignorePattern{}
		if strings.HasPrefix(line, "!") {
			parsed.negate = true
			compiled.hasNegations = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
			if line == "" {
				continue
			}
		}
		line = strings.TrimPrefix(path.Clean(line), "/")
		if line == "" || line == "." || line == ".." || strings.HasPrefix(line, "../") {
			continue
		}
		parsed.pattern = line
		compiled.patterns = append(compiled.patterns, parsed)
	}
	return compiled
}

// ignored reports whether rel (slash-separated, relative to the ignore root) is
// excluded. Patterns are evaluated in file order; a pattern matches when it
// matches the path or any of its ancestors, and the last matching pattern in
// file order decides. So `docs` + `!docs/keep.md` keeps `docs/keep.md`, while
// `!important/keep.txt` + `important` excludes `important/keep.txt` (the later
// ancestor exclusion wins) — both matching the Docker CLI.
func (d dockerIgnore) ignored(rel string) bool {
	prefixes := pathPrefixes(rel)
	excluded := false
	matched := false
	for _, p := range d.patterns {
		if !matchesAnyPrefix(p.pattern, prefixes) {
			continue
		}
		excluded = !p.negate
		matched = true
	}
	return matched && excluded
}

// pathPrefixes returns rel and each of its ancestors, shallowest first.
func pathPrefixes(rel string) []string {
	parts := strings.Split(rel, "/")
	prefixes := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		prefixes = append(prefixes, strings.Join(parts[:i], "/"))
	}
	return prefixes
}

// matchesAnyPrefix reports whether pattern matches any prefix.
func matchesAnyPrefix(pattern string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if matchContextPattern(pattern, prefix) {
			return true
		}
	}
	return false
}

// matchContextPattern matches a Docker ignore pattern against a
// context-relative path. `*` and `?` match within one path segment and `**`
// matches any number of segments (including none), which covers Docker's
// documented syntax. Escapes and `[`-classes beyond path.Match are not
// supported; a pattern that path.Match cannot parse simply does not match.
func matchContextPattern(pattern, rel string) bool {
	pattern = strings.TrimRight(pattern, "/")
	if pattern == "" {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

// matchSegments matches pattern segments against name segments with a
// bottom-up dynamic program, so a pattern with many `**` segments is
// O(len(pattern)*len(name)) rather than exponential in the number of
// wildcards. A `**` segment matches zero or more name segments.
func matchSegments(pattern, name []string) bool {
	dp := make([][]bool, len(pattern)+1)
	for i := range dp {
		dp[i] = make([]bool, len(name)+1)
	}
	dp[len(pattern)][len(name)] = true
	for i := len(pattern) - 1; i >= 0; i-- {
		for j := len(name); j >= 0; j-- {
			if pattern[i] == "**" {
				dp[i][j] = dp[i+1][j]
				if j < len(name) {
					dp[i][j] = dp[i][j] || dp[i][j+1]
				}
				continue
			}
			if j >= len(name) {
				continue
			}
			ok, err := path.Match(pattern[i], name[j])
			dp[i][j] = err == nil && ok && dp[i+1][j+1]
		}
	}
	return dp[0][0]
}
