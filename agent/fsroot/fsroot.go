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

var (
	listBatchSize = 512
	listScanCap   = 100_000
)

var (
	ErrInvalid    = errors.New("fsroot: invalid path")
	ErrEscape     = errors.New("fsroot: path escapes the root")
	ErrNotFound   = errors.New("fsroot: not found")
	ErrPermission = errors.New("fsroot: permission denied")
	ErrSpecial    = errors.New("fsroot: not a regular file or directory")
	ErrTooLarge   = errors.New("fsroot: directory too large")
	ErrExists     = errors.New("fsroot: already exists")
	ErrNotEmpty   = errors.New("fsroot: directory not empty")
	ErrConflict   = errors.New("fsroot: conflict")
	ErrReadOnly   = errors.New("fsroot: read-only root")
)

type Kind int

const (
	KindRegular Kind = iota
	KindDirectory
	KindSymlink
	KindSpecial
)

type Entry struct {
	Name          string
	Kind          Kind
	Size          int64
	Mode          os.FileMode
	Modified      time.Time
	SymlinkTarget string
	Etag          string
	UID           uint32
	GID           uint32
	Nlink         uint64
}

type Root struct {
	name     string
	path     string
	writable bool
	root     *os.Root
}

func Open(name, dir string, want os.FileInfo, writable bool) (*Root, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("fsroot: %q must be an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	real, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("fsroot: open %s: %w", dir, err)
	}
	if err := verifySameRoot(dir, want, real); err != nil {
		_ = real.Close()
		return nil, err
	}
	return &Root{name: name, path: dir, writable: writable, root: real}, nil
}

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

func (r *Root) Name() string { return r.name }

func (r *Root) Path() string { return r.path }

func (r *Root) Writable() bool { return r.writable }

func (r *Root) requireWritable() error {
	if !r.writable {
		return fmt.Errorf("%w: %s", ErrReadOnly, r.name)
	}
	return nil
}

func (r *Root) Close() error { return r.root.Close() }

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

func Clean(rel string) (string, error) { return cleanRel(rel) }

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

func mapError(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: %s: %v", ErrNotFound, op, err)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w: %s: %v", ErrPermission, op, err)
	case errors.Is(err, syscall.ENOTEMPTY):
		return fmt.Errorf("%w: %s: %v", ErrNotEmpty, op, err)
	case errors.Is(err, fs.ErrExist), errors.Is(err, syscall.EEXIST):
		return fmt.Errorf("%w: %s: %v", ErrExists, op, err)
	case errors.Is(err, os.ErrInvalid), errors.Is(err, fs.ErrInvalid):
		return fmt.Errorf("%w: %s: %v", ErrInvalid, op, err)
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.ENOTDIR):
		return fmt.Errorf("%w: %s: %v", ErrInvalid, op, err)
	case errors.Is(err, syscall.ENXIO):
		return fmt.Errorf("%w: %s: %v", ErrSpecial, op, err)
	case strings.Contains(err.Error(), "escapes from parent"):
		return fmt.Errorf("%w: %s: %v", ErrEscape, op, err)
	default:
		return fmt.Errorf("fsroot: %s: %w", op, err)
	}
}
