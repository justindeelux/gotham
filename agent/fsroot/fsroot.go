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
	"container/heap"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// listBatchSize is how many directory entries one ReadDir call returns, so a
// directory is streamed instead of being buffered whole. listScanCap bounds
// how many entries a single List scans; a directory with more entries than the
// cap is refused with ErrTooLarge rather than silently truncated, because a
// silently partial listing is indistinguishable from a complete one and would
// make the client page forever. Both are variables so a test can lower them.
var (
	listBatchSize = 512
	listScanCap   = 100_000
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
	// ErrTooLarge marks a directory holding more entries than one List scans.
	// It is an explicit failure, never a truncated page, so a client can tell a
	// directory it cannot enumerate from one it has finished enumerating.
	ErrTooLarge = errors.New("fsroot: directory too large")
	// ErrExists marks a name that already exists where an exclusive create or a
	// non-overwriting move/copy wanted it free.
	ErrExists = errors.New("fsroot: already exists")
	// ErrNotEmpty marks a directory that a non-recursive delete refused to
	// remove because it still holds entries. It is distinct from ErrExists so
	// the caller can report a precondition failure rather than a conflict.
	ErrNotEmpty = errors.New("fsroot: directory not empty")
	// ErrConflict marks a mutation whose precondition no longer holds, such as
	// an etag that changed under the caller. It is a retryable-by-the-user
	// conflict, not an internal failure.
	ErrConflict = errors.New("fsroot: conflict")
	// ErrReadOnly marks a mutation attempted on a root the caller opened
	// read-only. The refusal is configuration, not a filesystem error.
	ErrReadOnly = errors.New("fsroot: read-only root")
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
	// UID and Nlink are the platform identity needed to choose a write
	// strategy: a file the agent owns with a single link can be replaced by an
	// atomic rename, while a foreign, hard-linked or bind-mounted file must be
	// written in place so its inode, owner and ACLs survive.
	UID   uint32
	GID   uint32
	Nlink uint64
}

// Root is a confined view of one directory tree.
type Root struct {
	name     string
	path     string
	writable bool
	root     *os.Root
}

// Open confines dir, which must be an absolute path that already exists. want
// is the directory's identity captured during validation: the opened
// descriptor is compared against it so a directory swapped between the
// validation stat and this open is refused. An absolute path with a nil want
// is refused (the identity is required); nil is only meaningful for a call
// that fails the absolute-path check first.
func Open(name, dir string, want os.FileInfo, writable bool) (*Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("fsroot: %q must be an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	real, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("fsroot: open %s: %w", dir, err)
	}
	// Guard against the path being swapped between the caller's validation and
	// this open: the descriptor must still name the validated directory.
	if err := verifySameRoot(dir, want, real); err != nil {
		_ = real.Close()
		return nil, err
	}
	return &Root{name: name, path: dir, writable: writable, root: real}, nil
}

// verifySameRoot compares the opened descriptor with the identity captured
// during validation, aborting when a swap made them different directories. It
// must not stat the path again: that stat would race the open and could see
// the very replacement it is meant to reject.
func verifySameRoot(dir string, want os.FileInfo, root *os.Root) error {
	if want == nil {
		return fmt.Errorf("fsroot: %s was not validated", dir)
	}
	got, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("fsroot: stat opened root %s: %w", dir, err)
	}
	if !os.SameFile(want, got) {
		return fmt.Errorf("fsroot: %s changed while opening", dir)
	}
	return nil
}

// Name is the stable identifier the caller addresses this root by.
func (r *Root) Name() string { return r.name }

// Path is the absolute directory the root confines.
func (r *Root) Path() string { return r.path }

// Writable reports whether the caller may mutate entries under the root.
func (r *Root) Writable() bool { return r.writable }

// requireWritable refuses every mutation on a root opened read-only.
func (r *Root) requireWritable() error {
	if !r.writable {
		return fmt.Errorf("%w: %s", ErrReadOnly, r.name)
	}
	return nil
}

// Close releases the root's directory descriptor.
func (r *Root) Close() error { return r.root.Close() }

// List returns one page of the directory rel: up to limit entries sorted by
// name, skipping every name at or before cursor. truncated reports whether
// more entries follow the page and last is the cursor boundary: the last
// directory name of the page (not the last entry that survived its stat), so a
// page whose entries all vanished still advances the cursor.
//
// The directory is streamed in batches and only the smallest limit+1 names
// past the cursor are retained, so a directory with millions of entries never
// pins memory proportional to its size: memory is O(limit), time O(N) per
// call. listScanCap bounds N; exceeding it is an explicit ErrTooLarge, so a
// caller is never handed a silently partial listing.
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

	// Keep only the smallest limit+1 names greater than cursor in a max-heap:
	// the extra slot answers "is there another page?" without retaining the
	// rest of the directory.
	kept := &nameHeap{}
	heap.Init(kept)
	capacity := limit + 1
	scanned := 0
	for {
		batch, readErr := file.ReadDir(listBatchSize)
		for _, dirent := range batch {
			scanned++
			if scanned > listScanCap {
				return nil, false, "", fmt.Errorf("%w: more than %d entries in %s", ErrTooLarge, listScanCap, dir)
			}
			name := dirent.Name()
			if name <= cursor {
				continue
			}
			switch {
			case kept.Len() < capacity:
				heap.Push(kept, dirent)
			case name < (*kept)[0].Name():
				(*kept)[0] = dirent
				heap.Fix(kept, 0)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, false, "", mapError("list", readErr)
		}
		if len(batch) == 0 {
			break
		}
	}

	// ReadDir returns directory order, so sort the retained names.
	dirs := make([]os.DirEntry, kept.Len())
	copy(dirs, *kept)
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })

	page, truncated, last := selectPage(dirs, limit)
	entries, err = r.pageFromDirs(dir, page)
	if err != nil {
		return nil, false, "", err
	}
	return entries, truncated, last, nil
}

// selectPage truncates the sorted candidate entries to one page and reports
// the cursor boundary. The boundary is the last directory name in the page,
// taken from the directory listing rather than from the surviving entries, so
// a page whose entries all vanished between ReadDir and their stat still
// advances the cursor instead of repeating forever.
func selectPage(dirs []os.DirEntry, limit int) (page []os.DirEntry, truncated bool, last string) {
	if len(dirs) > limit {
		dirs = dirs[:limit]
		truncated = true
	}
	if truncated && len(dirs) > 0 {
		last = dirs[len(dirs)-1].Name()
	}
	return dirs, truncated, last
}

// nameHeap is a max-heap of directory entries ordered by name, used to retain
// the smallest names while streaming a directory.
type nameHeap []os.DirEntry

func (h nameHeap) Len() int           { return len(h) }
func (h nameHeap) Less(i, j int) bool { return h[i].Name() > h[j].Name() }
func (h nameHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *nameHeap) Push(x any) { *h = append(*h, x.(os.DirEntry)) }

func (h *nameHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return entry
}

// pageFromDirs builds the entries for a page, skipping any entry that vanished
// between the directory read and its stat, so one transient delete never
// aborts a whole listing.
func (r *Root) pageFromDirs(dir string, dirs []os.DirEntry) ([]Entry, error) {
	var entries []Entry
	for _, dirent := range dirs {
		entry, err := r.entry(dir, dirent)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
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

// Resolve resolves rel inside the root, following symbolic links, and returns
// the canonical root-relative path with no ".." or symlink component left, or
// an error when rel does not exist or a link would leave the root. It never
// follows an absolute link target or one that climbs above the root, so the
// result is always confined. Callers use it to decide safety on the resolved
// path rather than the caller-supplied one.
func (r *Root) Resolve(rel string) (string, error) {
	p, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if p == "." {
		return ".", nil
	}
	pending := strings.Split(p, "/")
	var resolved []string
	links := 0
	for len(pending) > 0 {
		comp := pending[0]
		pending = pending[1:]
		switch comp {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return "", fmt.Errorf("%w: %q escapes the root", ErrEscape, rel)
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		target := path.Join(strings.Join(resolved, "/"), comp)
		info, err := r.root.Lstat(target)
		if err != nil {
			return "", mapError("resolve", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > maxResolveLinks {
				return "", fmt.Errorf("%w: too many symbolic links resolving %q", ErrEscape, rel)
			}
			link, err := r.root.Readlink(target)
			if err != nil {
				return "", mapError("resolve", err)
			}
			if path.IsAbs(link) {
				return "", fmt.Errorf("%w: %q has an absolute symlink target", ErrEscape, rel)
			}
			pending = append(strings.Split(link, "/"), pending...)
			continue
		}
		if len(pending) > 0 && !info.IsDir() {
			return "", fmt.Errorf("%w: %q has a non-directory component", ErrInvalid, rel)
		}
		resolved = append(resolved, comp)
	}
	if len(resolved) == 0 {
		return ".", nil
	}
	return strings.Join(resolved, "/"), nil
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
	entry.UID, entry.GID, entry.Nlink = ownerLinks(info)
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

// Clean normalises a caller-supplied relative path the way every Root method
// does and rejects the shapes that must never reach the descriptor walk, so a
// caller can validate a path and derive its parent directory before it hands
// the path to a mutation method. It returns "." for an empty path.
func Clean(rel string) (string, error) { return cleanRel(rel) }

// Names streams every name in the directory rel to fn, in the directory's own
// order, stopping at the first error fn returns or a read error. It never
// follows a symbolic link, so a link planted in the tree cannot direct the walk
// outside the root. It is the streaming form of a directory scan, so a caller
// that bounds how many names it consumes never buffers a whole directory.
func (r *Root) Names(rel string, fn func(name string) error) error {
	p, err := cleanRel(rel)
	if err != nil {
		return err
	}
	file, err := r.root.OpenFile(p, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return mapError("open dir", err)
	}
	defer func() { _ = file.Close() }()
	for {
		batch, readErr := file.ReadDir(listBatchSize)
		for _, dirent := range batch {
			if err := fn(dirent.Name()); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return mapError("read dir", readErr)
		}
		if len(batch) == 0 {
			return nil
		}
	}
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
	case errors.Is(err, syscall.ENOTEMPTY):
		// A directory being removed or replaced is not empty: the caller's
		// precondition (empty target) failed. It must be tested before the
		// ErrExist case, because syscall.Errno.Is also matches ENOTEMPTY for
		// fs.ErrExist.
		return fmt.Errorf("%w: %s: %v", ErrNotEmpty, op, err)
	case errors.Is(err, fs.ErrExist), errors.Is(err, syscall.EEXIST):
		// A name that must be free already exists: the caller's conflict to
		// resolve.
		return fmt.Errorf("%w: %s: %v", ErrExists, op, err)
	case errors.Is(err, os.ErrInvalid), errors.Is(err, fs.ErrInvalid):
		return fmt.Errorf("%w: %s: %v", ErrInvalid, op, err)
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR):
		// A symlink loop or a non-directory component is a caller mistake, not
		// an internal failure, so it maps to the typed path error.
		return fmt.Errorf("%w: %s: %v", ErrInvalid, op, err)
	case errors.Is(err, syscall.ENXIO):
		// Opening a FIFO with no reader is a special file the service never
		// reads or writes.
		return fmt.Errorf("%w: %s: %v", ErrSpecial, op, err)
	case strings.Contains(err.Error(), "escapes from parent"):
		return fmt.Errorf("%w: %s: %v", ErrEscape, op, err)
	default:
		return fmt.Errorf("fsroot: %s: %w", op, err)
	}
}
