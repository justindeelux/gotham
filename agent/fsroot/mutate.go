package fsroot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const TempPrefix = ".gotham-tmp-"

const tempNameTries = 100

const maxResolveLinks = 40

var maxCopyDepth = 1024

var maxSweepDepth = 128

func (r *Root) CreateTemp(dir string, mode os.FileMode) (*os.File, string, error) {
	if err := r.requireWritable(); err != nil {
		return nil, "", err
	}
	d, err := cleanRel(dir)
	if err != nil {
		return nil, "", err
	}
	for range tempNameTries {
		name, err := randomName(d)
		if err != nil {
			return nil, "", err
		}
		file, openErr := r.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_CLOEXEC, mode)
		if openErr == nil {
			return file, name, nil
		}
		if !errors.Is(openErr, os.ErrExist) {
			return nil, "", mapError("create temp", openErr)
		}
	}
	return nil, "", fmt.Errorf("%w: could not create a temporary file", ErrExists)
}

func randomName(dir string) (string, error) {
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("fsroot: random temp name: %w", err)
	}
	return path.Join(dir, TempPrefix+hex.EncodeToString(suffix[:])), nil
}

// MkdirTemp creates one uniquely named directory under dir and returns its
// root-relative path. The name uses TempPrefix, so RemoveStaleTemps reclaims an
// abandoned one.
func (r *Root) MkdirTemp(dir string, mode os.FileMode) (string, error) {
	if err := r.requireWritable(); err != nil {
		return "", err
	}
	d, err := cleanRel(dir)
	if err != nil {
		return "", err
	}
	for range tempNameTries {
		name, err := randomName(d)
		if err != nil {
			return "", err
		}
		if mkdirErr := r.root.Mkdir(name, mode.Perm()); mkdirErr == nil {
			return name, nil
		} else if !errors.Is(mkdirErr, os.ErrExist) {
			return "", mapError("mkdir temp", mkdirErr)
		}
	}
	return "", fmt.Errorf("%w: could not create a temporary directory", ErrExists)
}

func (r *Root) OpenWrite(rel string) (*os.File, Entry, error) {
	if err := r.requireWritable(); err != nil {
		return nil, Entry{}, err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return nil, Entry{}, err
	}
	file, err := r.root.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
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

func (r *Root) Chmod(rel string, mode os.FileMode) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return err
	}
	if err := r.root.Chmod(p, mode.Perm()); err != nil {
		return mapError("chmod", err)
	}
	return nil
}

func (r *Root) ChmodEntry(rel string, mode os.FileMode, refuseHardlink bool) (Entry, error) {
	if err := r.requireWritable(); err != nil {
		return Entry{}, err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return Entry{}, err
	}
	if p == "." {
		return Entry{}, fmt.Errorf("%w: cannot chmod the root", ErrInvalid)
	}
	return r.chmodNoFollow(p, mode.Perm(), refuseHardlink)
}

func chmodTargetMode(stat *unix.Stat_t, p string, mode os.FileMode, refuseHardlink bool) (uint32, error) {
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		if refuseHardlink && stat.Nlink > 1 {
			return 0, fmt.Errorf("%w: %s has %d hard links; refusing to change a shared inode", ErrInvalid, p, stat.Nlink)
		}
	case unix.S_IFDIR:
		if mode.Perm()&0o100 == 0 {
			return 0, fmt.Errorf("%w: %s is a directory and must keep owner execute", ErrInvalid, p)
		}
	case unix.S_IFLNK:
		return 0, fmt.Errorf("%w: %s is a symbolic link", ErrInvalid, p)
	default:
		return 0, fmt.Errorf("%w: %s", ErrSpecial, p)
	}
	return uint32(mode.Perm()) | (uint32(stat.Mode) & 0o7000), nil
}

func (r *Root) Mkdir(rel string, mode os.FileMode) (Entry, error) {
	if err := r.requireWritable(); err != nil {
		return Entry{}, err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return Entry{}, err
	}
	if p == "." {
		return Entry{}, fmt.Errorf("%w: cannot create the root", ErrInvalid)
	}
	if err := r.root.Mkdir(p, mode.Perm()); err != nil {
		return Entry{}, mapError("mkdir", err)
	}
	if err := r.root.Chmod(p, mode.Perm()); err != nil {
		return Entry{}, mapError("chmod", err)
	}
	return r.Stat(p)
}

func (r *Root) CreateFile(rel string, mode os.FileMode, overwrite bool) (*os.File, error) {
	if err := r.requireWritable(); err != nil {
		return nil, err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	if p == "." {
		return nil, fmt.Errorf("%w: cannot create the root", ErrInvalid)
	}
	if entry, statErr := r.Stat(p); statErr == nil {
		if entry.Kind != KindRegular {
			return nil, fmt.Errorf("%w: %s is not a regular file", ErrSpecial, p)
		}
		if !overwrite {
			return nil, fmt.Errorf("%w: %s", ErrExists, p)
		}
	} else if !errors.Is(statErr, ErrNotFound) {
		return nil, statErr
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC | syscall.O_CLOEXEC
	if !overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL | syscall.O_CLOEXEC
	}
	file, err := r.root.OpenFile(p, flags, mode.Perm())
	if err != nil {
		return nil, mapError("create", err)
	}
	return file, nil
}

func (r *Root) MkdirAll(rel string, mode os.FileMode) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return err
	}
	if p == "." {
		return nil
	}
	if dir := path.Dir(p); dir != "." {
		entry, statErr := r.Stat(dir)
		switch {
		case statErr == nil:
			if entry.Kind != KindDirectory {
				return fmt.Errorf("%w: %s is not a directory", ErrInvalid, dir)
			}
		case errors.Is(statErr, ErrNotFound):
			if err := r.MkdirAll(dir, mode); err != nil {
				return err
			}
		default:
			return statErr
		}
	}
	entry, statErr := r.Stat(p)
	switch {
	case statErr == nil:
		if entry.Kind != KindDirectory {
			return fmt.Errorf("%w: %s is not a directory", ErrInvalid, p)
		}
		return nil
	case errors.Is(statErr, ErrNotFound):
		if _, err := r.Mkdir(p, mode); err != nil && !errors.Is(err, ErrExists) {
			return err
		}
		return nil
	default:
		return statErr
	}
}

func (r *Root) Rename(src, dst string) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	s, err := cleanRel(src)
	if err != nil {
		return err
	}
	d, err := cleanRel(dst)
	if err != nil {
		return err
	}
	if s == "." || d == "." {
		return fmt.Errorf("%w: cannot rename the root", ErrInvalid)
	}
	if err := r.root.Rename(s, d); err != nil {
		return mapError("rename", err)
	}
	return nil
}

func (r *Root) RenameExclusive(src, dst string) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	s, err := cleanRel(src)
	if err != nil {
		return err
	}
	d, err := cleanRel(dst)
	if err != nil {
		return err
	}
	if s == "." || d == "." {
		return fmt.Errorf("%w: cannot rename the root", ErrInvalid)
	}
	oldDir, err := r.root.OpenRoot(path.Dir(s))
	if err != nil {
		return mapError("rename", err)
	}
	defer func() { _ = oldDir.Close() }()
	newDir, err := r.root.OpenRoot(path.Dir(d))
	if err != nil {
		return mapError("rename", err)
	}
	defer func() { _ = newDir.Close() }()
	oldFd, err := openDirFd(oldDir)
	if err != nil {
		return mapError("rename", err)
	}
	defer func() { _ = oldFd.Close() }()
	newFd, err := openDirFd(newDir)
	if err != nil {
		return mapError("rename", err)
	}
	defer func() { _ = newFd.Close() }()

	err = renameNoReplace(int(oldFd.Fd()), path.Base(s), int(newFd.Fd()), path.Base(d))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL),
		errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENOTSUP):
		return r.renameExclusiveFallback(s, d)
	default:
		return mapError("rename", err)
	}
}

func openDirFd(dir *os.Root) (*os.File, error) {
	return dir.OpenFile(".", os.O_RDONLY|syscall.O_DIRECTORY, 0)
}

func (r *Root) renameExclusiveFallback(s, d string) error {
	entry, err := r.Stat(s)
	if err != nil {
		return err
	}
	if entry.Kind == KindDirectory {
		if _, err := r.Stat(d); err == nil {
			return fmt.Errorf("%w: %s", ErrExists, d)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		return r.Rename(s, d)
	}
	if err := r.root.Link(s, d); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return mapError("rename", err)
		}
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EPERM) {
			if _, err := r.Stat(d); err == nil {
				return fmt.Errorf("%w: %s", ErrExists, d)
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			return r.Rename(s, d)
		}
		return mapError("link", err)
	}
	return r.Remove(s)
}

func (r *Root) Link(oldname, newname string) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	old, err := cleanRel(oldname)
	if err != nil {
		return err
	}
	new, err := cleanRel(newname)
	if err != nil {
		return err
	}
	if err := r.root.Link(old, new); err != nil {
		return mapError("link", err)
	}
	return nil
}

func (r *Root) Symlink(target, newname string) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	new, err := cleanRel(newname)
	if err != nil {
		return err
	}
	if err := r.root.Symlink(target, new); err != nil {
		return mapError("symlink", err)
	}
	return nil
}

func (r *Root) Remove(rel string) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return err
	}
	if p == "." {
		return fmt.Errorf("%w: cannot remove the root", ErrInvalid)
	}
	if err := r.root.Remove(p); err != nil {
		return mapError("remove", err)
	}
	return nil
}

func (r *Root) RemoveTree(ctx context.Context, rel string, onEntry func(string, Entry) error) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	p, err := cleanRel(rel)
	if err != nil {
		return err
	}
	if p == "." {
		return fmt.Errorf("%w: cannot remove the root", ErrInvalid)
	}
	return r.removeTree(ctx, p, onEntry)
}

func (r *Root) removeTree(ctx context.Context, p string, onEntry func(string, Entry) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := r.Stat(p)
	if err != nil {
		return err
	}
	if entry.Kind == KindDirectory {
		names, err := r.dirNames(p)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.removeTree(ctx, path.Join(p, name), onEntry); err != nil {
				return err
			}
		}
	}
	if err := r.root.Remove(p); err != nil {
		return mapError("remove", err)
	}
	if onEntry != nil {
		if err := onEntry(p, entry); err != nil {
			return err
		}
	}
	return nil
}

func (r *Root) RemoveStaleTemps(prefix string, cutoff time.Time) (int, error) {
	if err := r.requireWritable(); err != nil {
		return 0, err
	}
	removed, errs := r.removeStaleTemps(".", prefix, cutoff, 0)
	return removed, errors.Join(errs...)
}

func (r *Root) removeStaleTemps(dir, prefix string, cutoff time.Time, depth int) (int, []error) {
	if depth > maxSweepDepth {
		return 0, nil
	}
	names, err := r.dirNames(dir)
	if err != nil {
		return 0, []error{err}
	}
	removed := 0
	var errs []error
	for _, name := range names {
		rel := path.Join(dir, name)
		entry, err := r.Stat(rel)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				errs = append(errs, err)
			}
			continue
		}
		stale := strings.HasPrefix(name, prefix) && entry.Modified.Before(cutoff)
		if entry.Kind == KindDirectory {
			if stale {
				if err := r.removeTree(context.Background(), rel, nil); err != nil {
					errs = append(errs, err)
					continue
				}
				removed++
				continue
			}
			n, subErrs := r.removeStaleTemps(rel, prefix, cutoff, depth+1)
			removed += n
			errs = append(errs, subErrs...)
			continue
		}
		if !stale {
			continue
		}
		if err := r.root.Remove(rel); err != nil {
			errs = append(errs, mapError("remove", err))
			continue
		}
		removed++
	}
	return removed, errs
}

func (r *Root) Copy(ctx context.Context, src, dst string, overwrite bool, onEntry func(string, Entry) error) error {
	if err := r.requireWritable(); err != nil {
		return err
	}
	s, err := cleanRel(src)
	if err != nil {
		return err
	}
	d, err := cleanRel(dst)
	if err != nil {
		return err
	}
	if s == "." || d == "." {
		return fmt.Errorf("%w: cannot copy the root", ErrInvalid)
	}
	if s == d || withinRel(s, d) {
		return fmt.Errorf("%w: cannot copy %q into itself", ErrInvalid, src)
	}
	if err := r.refuseCopyIntoItself(s, d); err != nil {
		return err
	}
	if !overwrite {
		if _, statErr := r.Stat(d); statErr == nil {
			return fmt.Errorf("%w: %s", ErrExists, d)
		} else if !errors.Is(statErr, ErrNotFound) {
			return statErr
		}
	}
	return r.copyStaged(ctx, s, d, overwrite, onEntry)
}

func (r *Root) refuseCopyIntoItself(s, d string) error {
	resolvedSrc, err := r.Resolve(s)
	if err != nil {
		return err
	}
	resolvedParent, err := r.Resolve(path.Dir(d))
	if err != nil {
		return nil
	}
	resolvedDst := path.Join(resolvedParent, path.Base(d))
	if resolvedDst == resolvedSrc || withinRel(resolvedDst, resolvedSrc) || withinRel(resolvedSrc, resolvedDst) {
		return fmt.Errorf("%w: cannot copy %q into itself", ErrInvalid, s)
	}
	if same, err := r.sameEntry(s, d); err == nil && same {
		return fmt.Errorf("%w: cannot copy %q onto itself", ErrInvalid, s)
	}
	return nil
}

func (r *Root) sameEntry(a, b string) (bool, error) {
	ai, err := r.root.Stat(a)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, mapError("stat", err)
	}
	bi, err := r.root.Stat(b)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, mapError("stat", err)
	}
	return os.SameFile(ai, bi), nil
}

func (r *Root) copyStaged(ctx context.Context, src, dst string, overwrite bool, onEntry func(string, Entry) error) error {
	stage, err := randomName(path.Dir(dst))
	if err != nil {
		return err
	}
	report := func(rel string, e Entry) error {
		if onEntry == nil {
			return nil
		}
		return onEntry(stagedPath(stage, dst, rel), e)
	}
	if err := r.copyTree(ctx, src, stage, report, 0); err != nil {
		r.removeStaged(stage)
		return err
	}
	if overwrite {
		err = r.root.Rename(stage, dst)
	} else {
		err = r.RenameExclusive(stage, dst)
	}
	if err != nil {
		r.removeStaged(stage)
		return mapError("rename", err)
	}
	return nil
}

func (r *Root) removeStaged(p string) {
	_ = r.RemoveTree(context.Background(), p, nil)
}

func stagedPath(stage, dst, rel string) string {
	if rel == stage {
		return dst
	}
	if suffix, ok := strings.CutPrefix(rel, stage+"/"); ok {
		return path.Join(dst, suffix)
	}
	return rel
}

func (r *Root) copyTree(ctx context.Context, src, dst string, onEntry func(string, Entry) error, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxCopyDepth {
		return fmt.Errorf("%w: copy nesting exceeds %d levels", ErrTooLarge, maxCopyDepth)
	}
	entry, err := r.Stat(src)
	if err != nil {
		return err
	}
	switch entry.Kind {
	case KindDirectory:
		if err := r.root.Mkdir(dst, entry.Mode.Perm()|0o700); err != nil {
			return mapError("mkdir", err)
		}
		names, err := r.dirNames(src)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.copyTree(ctx, path.Join(src, name), path.Join(dst, name), onEntry, depth+1); err != nil {
				return err
			}
		}
		if err := r.root.Chmod(dst, entry.Mode.Perm()); err != nil {
			return mapError("chmod", err)
		}
	case KindSymlink:
		if err := r.root.Symlink(entry.SymlinkTarget, dst); err != nil {
			return mapError("symlink", err)
		}
	case KindRegular:
		if err := r.copyFile(ctx, src, dst, entry.Mode.Perm()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: cannot copy %s", ErrSpecial, src)
	}
	if onEntry != nil {
		if err := onEntry(dst, entry); err != nil {
			return err
		}
	}
	return nil
}

func (r *Root) copyFile(ctx context.Context, src, dst string, mode os.FileMode) error {
	in, _, err := r.OpenRegular(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := r.root.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return mapError("create", err)
	}
	if err := copyWithContext(ctx, out, in); err != nil {
		_ = out.Close()
		_ = r.root.Remove(dst)
		return mapError("copy", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = r.root.Remove(dst)
		return mapError("sync", err)
	}
	if err := out.Close(); err != nil {
		_ = r.root.Remove(dst)
		return mapError("close", err)
	}
	if err := r.root.Chmod(dst, mode.Perm()); err != nil {
		return mapError("chmod", err)
	}
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) error {
	buffer := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func (r *Root) dirNames(rel string) ([]string, error) {
	file, err := r.root.Open(rel)
	if err != nil {
		return nil, mapError("open dir", err)
	}
	defer func() { _ = file.Close() }()
	var names []string
	for {
		batch, readErr := file.ReadDir(listBatchSize)
		for _, dirent := range batch {
			names = append(names, dirent.Name())
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return names, nil
			}
			return nil, mapError("read dir", readErr)
		}
	}
}

func withinRel(parent, child string) bool {
	return strings.HasPrefix(child, parent+"/")
}
