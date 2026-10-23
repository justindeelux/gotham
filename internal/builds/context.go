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

const defaultMaxContextBytes = int64(512 << 20)

const maxDockerIgnoreBytes = int64(1 << 20)

const dockerIgnoreFile = ".dockerignore"

var excludedContextEntries = map[string]bool{
	".git":      true,
	".DS_Store": true,
	"__MACOSX":  true,
}

type contextSpec struct {
	root      string
	ignoreDir string
	extra     map[string][]byte
	maxBytes  int64
	keep      string
}

func buildContextTar(spec contextSpec) ([]byte, error) {
	data, _, err := buildContextTarPeak(spec)
	return data, err
}

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
	prefix := ignorePrefix(spec.ignoreDir, dir)

	var buf bytes.Buffer
	limited := &limitedWriter{writer: &buf, limit: limit}
	tw := tar.NewWriter(limited)
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
			excluded = ignore.excludedOnly(rebased(prefix, name), parentState)
		}
		protected := spec.keep != "" &&
			(name == spec.keep || strings.HasPrefix(spec.keep, name+"/"))
		if !protected && excluded {
			if isDir && !ignore.hasNegations {
				return filepath.SkipDir
			}
			return nil
		}
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

func rebased(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

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

type limitedWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.written+int64(len(p)) > w.limit {
		return 0, fmt.Errorf("%w: build context exceeds %d bytes", ErrValidation, w.limit)
	}
	n, err := w.writer.Write(p)
	w.written += int64(n)
	return n, err
}

type dockerIgnore struct {
	patterns     []ignorePattern
	hasNegations bool
}

type ignorePattern struct {
	pattern string
	negate  bool
}

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

func (d dockerIgnore) matchState(rel string, parent []bool) (bool, []bool) {
	state := make([]bool, len(d.patterns))
	excluded := d.match(rel, parent, state)
	return excluded, state
}

func (d dockerIgnore) excludedOnly(rel string, parent []bool) bool {
	return d.match(rel, parent, nil)
}

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

type dirStateChain struct {
	states [][]bool
	total  int
	peak   int
}

func (c *dirStateChain) parent(depth int) []bool {
	if depth > 0 && depth-1 < len(c.states) {
		return c.states[depth-1]
	}
	return nil
}

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

func (c *dirStateChain) peakRetained() int {
	return c.peak
}

func (d dockerIgnore) ignoredPath(rel string) bool {
	var parent []bool
	excluded := false
	for _, prefix := range pathPrefixes(rel) {
		excluded, parent = d.matchState(prefix, parent)
	}
	return excluded
}

func pathPrefixes(rel string) []string {
	parts := strings.Split(rel, "/")
	prefixes := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		prefixes = append(prefixes, strings.Join(parts[:i], "/"))
	}
	return prefixes
}

func matchContextPattern(pattern, rel string) bool {
	pattern = strings.TrimRight(pattern, "/")
	if pattern == "" {
		return false
	}
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok && prefix != "" {
		if prefix == "**" || strings.HasSuffix(prefix, "/**") {
			return matchContextPattern(prefix, rel)
		}
		if !strings.ContainsAny(prefix, "*?[") {
			return strings.HasPrefix(rel, prefix+"/")
		}
		return matchesProperAncestor(prefix, rel)
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

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
