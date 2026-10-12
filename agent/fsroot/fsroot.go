// Package fsroot provides a filesystem root confined to one directory tree.
//
// It wraps os.OpenRoot: every path is resolved relative to a held directory
// descriptor, so a caller can hand it control-plane-supplied relative paths
// without them escaping the tree — a ".." component, an absolute path or a
// symlink whose target lies outside the root is refused by the descriptor
// walk, not by string inspection. Relative paths that stay inside the root are
// resolved normally, including symlinks.
//
// Every open is a no-follow walk (os.Root adds O_NOFOLLOW and O_CLOEXEC on
// each component) plus the caller's O_NONBLOCK; the result is fstat-ed so a
// FIFO, socket or device is reported as KindSpecial instead of being read.
// Only regular files and directories are ever read.
package fsroot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Sentinel errors a caller maps onto its own vocabulary. They wrap the
// underlying os error, so errors.Is sees both.
var (
	// ErrInvalid marks a path the caller must fix: not relative, or containing
	// a ".." component.
	ErrInvalid = errors.New("fsroot: invalid path")
	// ErrEscape marks a path that tries to leave the root, including a symlink
	// whose target lies outside it.
	ErrEscape = errors.New("fsroot: path escapes the root")
	// ErrNotFound marks a missing entry.
	ErrNotFound = errors.New("fsroot: not found")
	// ErrPermission marks an entry the process may not read.
	ErrPermission = errors.New("fsroot: permission denied")
	// ErrSpecial marks an entry that is neither a regular file nor a directory
	// and therefore cannot be read or written.
	ErrSpecial = errors.New("fsroot: not a regular file or directory")
)

// Kind classifies a directory entry.
type Kind int

// Entry kinds. Anything that is not a regular file, a directory or a symlink
// is KindSpecial.
const (
	KindRegular Kind = iota
	KindDirectory
	KindSymlink
	KindSpecial
)

// Entry is one directory entry's metadata.
type Entry struct {
	Name          string
	Kind          Kind
	Size          int64
	Mode          os.FileMode
	Modified      time.Time
	SymlinkTarget string
	Etag          string
}

// Root is a confined view of one directory tree.
type Root struct {
	name     string
	path     string
	writable bool
	root     *os.Root
}

// Open confines dir, which must be an absolute path that already exists.
func Open(name, dir string, writable bool) (*Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("fsroot: %q must be an absolute path", dir)
	}
	real, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("fsroot: open %s: %w", dir, err)
	}
	return &Root{name: name, path: filepath.Clean(dir), writable: writable, root: real}, nil
}

// Name is the stable identifier the caller addresses this root by.
func (r *Root) Name() string { return r.name }

// Path is the absolute directory the root confines.
func (r *Root) Path() string { return r.path }

// Writable reports whether the caller may mutate entries under the root.
func (r *Root) Writable() bool { return r.writable }

// Close releases the root's directory descriptor.
func (r *Root) Close() error { return r.root.Close() }

// List returns one page of the directory rel: up to limit entries sorted by
// name, skipping every name at or before cursor. truncated reports whether
// more entries follow the page and cursor is the last returned name.
func (r *Root) List(rel, cursor string, limit int) (entries []Entry, truncated bool, last string, err error) {
	if limit <= 0 {
		limit = 1
	}
	dir, err := cleanRel(rel)
	if err != nil {
		return nil, false, "", err
	}
	file, err := r.root.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, "", mapError("list", err)
	}
	defer func() { _ = file.Close() }()

	dirents, err := file.ReadDir(-1)
	if err != nil {
		return nil, false, "", mapError("list", err)
	}
	// ReadDir already sorts, but a page must never depend on that.
	sort.Slice(dirents, func(i, j int) bool { return dirents[i].Name() < dirents[j].Name() })

	start := 0
	if cursor != "" {
		start = sort.Search(len(dirents), func(i int) bool { return dirents[i].Name() > cursor })
	}
	end := min(start+limit, len(dirents))
	for _, dirent := range dirents[start:end] {
		entry, err := r.entry(dir, dirent)
		if err != nil {
			return nil, false, "", err
		}
		entries = append(entries, entry)
	}
	truncated = end < len(dirents)
	if truncated && len(entries) > 0 {
		last = entries[len(entries)-1].Name
	}
	return entries, truncated, last, nil
}

// Stat returns the metadata of rel without following a final symlink, so a
// link is reported as KindSymlink alongside its target.
func (r *Root) Stat(rel string) (Entry, error) {
	p, err := cleanRel(rel)
	if err != nil {
		return Entry{}, err
	}
	info, err := r.root.Lstat(p)
	if err != nil {
		return Entry{}, mapError("stat", err)
	}
	return r.entryFor(p, info)
}

// OpenRegular opens rel for reading and returns the open file and its entry.
// It never follows a path out of the root and refuses anything that is not a
// regular file, so a device, FIFO or socket can never block or be read.
func (r *Root) OpenRegular(rel string) (*os.File, Entry, error) {
	p, err := cleanRel(rel)
	if err != nil {
		return nil, Entry{}, err
	}
	file, err := r.root.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, Entry{}, mapError("open", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, Entry{}, mapError("stat", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, Entry{}, fmt.Errorf("%w: %s", ErrSpecial, p)
	}
	entry, err := r.entryFor(p, info)
	if err != nil {
		_ = file.Close()
		return nil, Entry{}, err
	}
	return file, entry, nil
}

// entry builds an Entry for one directory entry, which is already lstat-ed by
// the ReadDir walk.
func (r *Root) entry(dir string, dirent os.DirEntry) (Entry, error) {
	info, err := dirent.Info()
	if err != nil {
		return Entry{}, mapError("stat", err)
	}
	entry, err := r.entryFor(path.Join(dir, dirent.Name()), info)
	if err != nil {
		return Entry{}, err
	}
	entry.Name = dirent.Name()
	return entry, nil
}

// entryFor builds an Entry from lstat info. p is the path relative to the
// root; info must be the link's own info, not its target's.
func (r *Root) entryFor(p string, info os.FileInfo) (Entry, error) {
	entry := Entry{
		Name:     path.Base(strings.TrimSuffix(p, "/")),
		Size:     info.Size(),
		Mode:     info.Mode().Perm(),
		Modified: info.ModTime(),
		Etag:     etag(info),
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		entry.Kind = KindSymlink
		if target, err := r.root.Readlink(p); err == nil {
			entry.SymlinkTarget = target
		}
	case info.IsDir():
		entry.Kind = KindDirectory
	case mode.IsRegular():
		entry.Kind = KindRegular
	default:
		entry.Kind = KindSpecial
	}
	return entry, nil
}

// cleanRel normalises a caller-supplied relative path and rejects the shapes
// that must never reach the descriptor walk. os.Root would also refuse them,
// but a typed error is clearer than a wrapped ELOOP/ENOENT.
func cleanRel(rel string) (string, error) {
	if strings.IndexByte(rel, 0) >= 0 {
		return "", fmt.Errorf("%w: path contains a NUL byte", ErrInvalid)
	}
	if rel == "" {
		return ".", nil
	}
	if filepath.IsAbs(rel) || path.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q is absolute", ErrInvalid, rel)
	}
	cleaned := path.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: %q escapes the root", ErrInvalid, rel)
	}
	return cleaned, nil
}

// mapError folds an os error into the package sentinels. os.Root reports an
// escape as a "path escapes from parent" *PathError, which has no exported
// sentinel, so it is recognised by its text.
func mapError(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: %s: %v", ErrNotFound, op, err)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w: %s: %v", ErrPermission, op, err)
	case errors.Is(err, os.ErrInvalid), errors.Is(err, fs.ErrInvalid):
		return fmt.Errorf("%w: %s: %v", ErrInvalid, op, err)
	case strings.Contains(err.Error(), "escapes from parent"):
		return fmt.Errorf("%w: %s: %v", ErrEscape, op, err)
	default:
		return fmt.Errorf("fsroot: %s: %w", op, err)
	}
}
