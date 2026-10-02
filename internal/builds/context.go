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
	data, _, err := buildContextTarPeak(spec)
	return data, err
}

// buildContextTarPeak is buildContextTar plus the peak number of retained
// per-pattern match-state values, which the bounded-memory regression test
// asserts stays O(tree depth).
func buildContextTarPeak(spec contextSpec) ([]byte, int, error) {
	dir := spec.root
	info, err := os.Stat(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: stat %s: %v", ErrValidation, dir, err)
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("%w: %s is not a directory", ErrValidation, dir)
	}

	limit := spec.maxBytes
	if limit <= 0 {
		limit = defaultMaxContextBytes
	}
	ignore, err := loadDockerIgnore(spec.ignoreDir, dir)
	if err != nil {
		return nil, 0, err
	}
	// Patterns come from ignoreDir but paths are packed relative to root, so
	// match against the path rebased onto ignoreDir.
	prefix := ignorePrefix(spec.ignoreDir, dir)

	var buf bytes.Buffer
	limited := &limitedWriter{writer: &buf, limit: limit}
	tw := tar.NewWriter(limited)
	// Per-pattern match state for the current ancestor chain only, indexed by
	// walk depth, so retained memory is O(tree depth × patterns) rather than
	// O(directories × patterns).
	var dirStates dirStateChain

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
		depth := strings.Count(name, "/")
		parentState := dirStates.parent(depth)
		var excluded bool
		if isDir {
			var state []bool
			excluded, state = ignore.matchState(rebased(prefix, name), parentState)
			dirStates.set(depth, state)
		} else {
			// A file has no children, so only its decision is needed; skip the
			// per-pattern state allocation.
			excluded = ignore.excludedOnly(rebased(prefix, name), parentState)
		}
		// The Dockerfile and its parent directories are never filtered: the
		// builder must be able to read it even when .dockerignore lists it.
		protected := spec.keep != "" &&
			(name == spec.keep || strings.HasPrefix(spec.keep, name+"/"))
		if !protected && excluded {
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
		return nil, 0, fmt.Errorf("builds: pack context %s: %w", dir, err)
	}

	for _, name := range sortedKeys(spec.extra) {
		content := spec.extra[name]
		if err := tw.WriteHeader(&tar.Header{
			Name:     filepath.ToSlash(name),
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, 0, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, 0, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), dirStates.peakRetained(), nil
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
	if resolved, ok := contextRelativeTarget(name, target); ok && ignore.ignoredPath(rebased(prefix, resolved)) {
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
// followed (the Docker CLI does) but only when it resolves inside the ignore
// directory, so a hostile repository cannot make the control plane read an
// arbitrary host file as patterns. A non-regular target (a device, FIFO or
// directory) is treated as absent, and the read is capped so a huge file cannot
// exhaust memory.
func loadDockerIgnore(ignoreDir, root string) (dockerIgnore, error) {
	if strings.TrimSpace(ignoreDir) == "" {
		ignoreDir = root
	}
	fullPath := filepath.Join(ignoreDir, dockerIgnoreFile)
	resolved, err := filepath.EvalSymlinks(fullPath)
	if errors.Is(err, fs.ErrNotExist) {
		return dockerIgnore{}, nil
	}
	if err != nil {
		return dockerIgnore{}, fmt.Errorf("builds: resolve %s: %w", dockerIgnoreFile, err)
	}
	base, err := filepath.EvalSymlinks(ignoreDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dockerIgnore{}, nil
		}
		return dockerIgnore{}, fmt.Errorf("builds: resolve ignore root: %w", err)
	}
	if rel, relErr := filepath.Rel(base, resolved); relErr != nil || !filepath.IsLocal(rel) {
		// The followed symlink escapes the repository: ignore it.
		return dockerIgnore{}, nil
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return dockerIgnore{}, nil
	}
	if err != nil {
		return dockerIgnore{}, fmt.Errorf("builds: stat %s: %w", dockerIgnoreFile, err)
	}
	if !info.Mode().IsRegular() {
		return dockerIgnore{}, nil
	}
	file, err := os.Open(resolved)
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

// matchState reports whether rel is excluded and returns the per-pattern match
// state to thread to rel's children. It replicates
// moby/patternmatcher.MatchesUsingParentResults: a pattern already matched by an
// ancestor stays matched (inherited), the state machine evaluates a negation
// only while the path is excluded and a normal pattern only while it is not,
// and the last matching pattern in file order decides. This is what makes
// `logs/secret.log` + `!logs` exclude the file while `important` +
// `!important/keep.txt` keep it — both verified against the Docker CLI.
func (d dockerIgnore) matchState(rel string, parent []bool) (bool, []bool) {
	state := make([]bool, len(d.patterns))
	excluded := d.match(rel, parent, state)
	return excluded, state
}

// excludedOnly is matchState without allocating the per-pattern state, for
// entries (files and symlinks) whose children never need it.
func (d dockerIgnore) excludedOnly(rel string, parent []bool) bool {
	return d.match(rel, parent, nil)
}

// match is the shared state machine. It writes each pattern's result into state
// when state is non-nil; a nil state keeps the file path allocation-free.
func (d dockerIgnore) match(rel string, parent, state []bool) bool {
	excluded := false
	for i, p := range d.patterns {
		matched := i < len(parent) && parent[i]
		if !matched {
			if p.negate != excluded {
				continue
			}
			matched = matchContextPattern(p.pattern, rel)
		}
		if state != nil {
			state[i] = matched
		}
		if matched {
			excluded = !p.negate
		}
	}
	return excluded
}

// dirStateChain keeps the per-pattern match state of the current directory
// ancestors only, indexed by walk depth. A depth-first walk reads a parent
// state at depth-1 and writes its own at depth, so only one state per depth is
// ever live and memory is O(tree depth), not O(number of directories).
type dirStateChain struct {
	states [][]bool
	total  int
	peak   int
}

// parent returns the match state of the directory at depth-1 (nil at the root).
func (c *dirStateChain) parent(depth int) []bool {
	if depth > 0 && depth-1 < len(c.states) {
		return c.states[depth-1]
	}
	return nil
}

// set stores the match state for a directory at the given depth, replacing any
// stale state at that depth from a previous subtree.
func (c *dirStateChain) set(depth int, state []bool) {
	for len(c.states) <= depth {
		c.states = append(c.states, nil)
	}
	c.total -= len(c.states[depth])
	c.states[depth] = state
	c.total += len(state)
	if c.total > c.peak {
		c.peak = c.total
	}
}

// peakRetained reports the greatest number of match-state values held at once,
// which the bounded-memory test asserts stays O(tree depth × patterns).
func (c *dirStateChain) peakRetained() int {
	return c.peak
}

// ignoredPath reports whether a standalone path (for example a symlink's
// resolved target) is excluded, threading the ancestor states from the root.
func (d dockerIgnore) ignoredPath(rel string) bool {
	var parent []bool
	excluded := false
	for _, prefix := range pathPrefixes(rel) {
		excluded, parent = d.matchState(prefix, parent)
	}
	return excluded
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

// matchContextPattern matches a Docker ignore pattern against a
// context-relative path. `*` and `?` match within one path segment and `**`
// matches any number of segments (including none), which covers Docker's
// documented syntax. A trailing `/**` matches a proper ancestor of rel (moby's
// prefixMatch / `prefix/.*` regexp), so it matches everything under the prefix
// but never a path equal to the prefix itself. Escapes and `[`-classes beyond
// path.Match are not supported; a pattern that path.Match cannot parse simply
// does not match.
func matchContextPattern(pattern, rel string) bool {
	pattern = strings.TrimRight(pattern, "/")
	if pattern == "" {
		return false
	}
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok && prefix != "" {
		// A prefix already ending in a ** segment has absorbed the separator,
		// so the pattern is equivalent to that prefix.
		if prefix == "**" || strings.HasSuffix(prefix, "/**") {
			return matchContextPattern(prefix, rel)
		}
		return matchesProperAncestor(prefix, rel)
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

// matchesProperAncestor reports whether prefix matches a proper ancestor of
// rel, so a trailing /** never matches rel itself.
func matchesProperAncestor(prefix, rel string) bool {
	segments := strings.Split(rel, "/")
	pattern := strings.Split(prefix, "/")
	for i := 1; i < len(segments); i++ {
		if matchSegments(pattern, segments[:i]) {
			return true
		}
	}
	return false
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
