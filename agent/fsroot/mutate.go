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

// TempPrefix is the base-name prefix of every staging file fsroot creates. It
// is exported so a caller can sweep leaked staging files it owns (the agent's
// file service does this at startup and periodically).
const TempPrefix = ".gotham-tmp-"

// tempNameTries bounds how many random temporary names CreateTemp tries before
// giving up; a collision this deep would mean something is very wrong.
const tempNameTries = 100

// maxResolveLinks bounds how many symbolic links Resolve follows before it
// gives up, the same ceiling a kernel ELOOP enforces.
const maxResolveLinks = 40

// maxCopyDepth bounds how deep a recursive copy may descend. A healthy tree is
// far shallower; the bound turns a symlink-fuelled self-copy into an explicit
// error instead of unbounded recursion. It is a variable so a test can lower it.
var maxCopyDepth = 1024

// maxSweepDepth bounds how deep the staging-file sweeper descends, so its
// hourly walk of a large volume tree stays cheap. A staging file buried deeper
// than the cap is left for a manual sweep. It is a variable so a test can lower
// it.
var maxSweepDepth = 128

// CreateTemp creates a uniquely named temporary regular file inside dir (a
// directory relative to the root) and returns it together with its
// root-relative name. The name is random and O_EXCL, so it never collides with
// a caller's file. The caller owns the file and must remove it when it is not
// committed.
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

// randomName returns a path inside dir with a fresh random base name.
func randomName(dir string) (string, error) {
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("fsroot: random temp name: %w", err)
	}
	return path.Join(dir, TempPrefix+hex.EncodeToString(suffix[:])), nil
}

// OpenWrite opens an existing regular file for writing without truncating it,
// returning the file and its entry so the caller can check the etag before it
// commits. A directory, special file or a path that escapes the root is
// refused; a symlink inside the root is followed to its target, which must
// itself be a regular file.
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

// Chmod sets one entry's permission bits (0o777 mask). It is used to give a
// freshly created temporary file the requested or inherited mode. It follows a
// final symbolic link to its target (like os.Root.Chmod); the user-facing
// chmod is ChmodEntry, which refuses a link instead.
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

// ChmodEntry sets the permission bits of one entry under the root and returns
// the updated entry. It is the user-facing chmod: a final symbolic link is
// refused rather than followed (the link's target's mode is never touched), a
// special file (FIFO, socket, device) is refused, and the root itself is
// refused.
//
// Only mode.Perm() (the low 0o777 bits) is applied. The caller is responsible
// for rejecting a requested setuid/setgid/sticky bit; existing special bits are
// preserved (never created) so "only plain permission bits change" holds. A
// directory must keep its owner execute bit so it stays traversable. When
// refuseHardlink is set (a confined root, as opposed to the whole-filesystem
// root), a regular file with more than one hard link is refused: its inode may
// also be reachable outside the root, so changing its mode would change that
// other name too. ChmodEntry never changes an entry's type, so a directory
// (including a managed application directory) stays a directory. The returned
// entry is read from the same descriptor, so an entry under a directory the
// agent cannot read (0o111 or 0o000) is still chmod-able and reported.
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

// chmodTargetMode classifies the opened entry from its fstat and returns the
// mode to write: the caller's plain permission bits OR-ed with the special bits
// already set (preserved, never created). It refuses a symbolic link
// (ErrInvalid, so the control plane answers 400), a special file (ErrSpecial),
// a directory that would lose its owner execute bit (ErrInvalid: the entry
// would no longer be traversable), and — when refuseHardlink is set — a regular
// file with more than one hard link (ErrInvalid).
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

// Mkdir creates one directory and returns its entry. It is exclusive: a name
// that already exists is ErrExists.
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
	// Mkdir is subject to the process umask; set the exact mode the caller
	// asked for, as the file manager presents mode as an explicit choice.
	if err := r.root.Chmod(p, mode.Perm()); err != nil {
		return Entry{}, mapError("chmod", err)
	}
	return r.Stat(p)
}

// CreateFile opens rel for writing, creating it when it is missing. An existing
// entry that is not a regular file (a directory, symlink or special file) is
// refused, so a create never writes through a link. When overwrite is false an
// existing name is ErrExists (the create is exclusive and race-safe via O_EXCL);
// when true an existing regular file is truncated. The caller sets the exact
// mode afterwards with Chmod if it needs bits the process umask would drop.
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

// MkdirAll creates rel and every missing parent, each with mode. An existing
// path is reused when it is a directory and refused otherwise, so extracting an
// archive whose directory entries are absent still lands inside the root, while
// a file that already occupies a directory name is never overwritten.
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

// Rename moves src to dst within the root. os.Root.Rename replaces dst
// atomically; the caller enforces the overwrite policy and any app-directory
// protection beforehand. A cross-device rename surfaces the raw EXDEV so the
// caller can fall back to a copy.
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

// RenameExclusive moves src to dst within the root and refuses when dst already
// exists, even against a concurrent creator: it is the non-replacing rename the
// overwrite=false policy needs. On Linux it uses renameat2(RENAME_NOREPLACE);
// elsewhere it falls back to a hard link then unlink for a non-directory, or to
// a stat-checked rename for a directory (no portable exclusive directory rename
// exists). A cross-device move surfaces the wrapped EXDEV for the caller's
// guarded copy fallback.
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

// openDirFd returns a descriptor for a directory root, so a confined
// renameat2 can run relative to it.
func openDirFd(dir *os.Root) (*os.File, error) {
	return dir.OpenFile(".", os.O_RDONLY|syscall.O_DIRECTORY, 0)
}

// renameExclusiveFallback implements a non-replacing rename without
// renameat2: a hard link then unlink for a non-directory, and a stat-checked
// rename for a directory. The directory case keeps the old stat-then-rename
// window on platforms without renameat2, which the agent does not target for
// writes.
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
			// A filesystem without hard links: fall back to a stat-checked
			// rename so such a filesystem can still create exclusively.
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

// Link creates newname as a hard link to oldname, failing with ErrExists when
// newname already occupies the name. It is the no-replace primitive an
// exclusive create uses, so a file that appears after the caller checked is
// still refused rather than overwritten.
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

// Symlink creates newname (relative to the root) as a symbolic link to target.
// The target text is copied verbatim, so a link that pointed inside its old
// root keeps the same text.
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

// Remove deletes one entry: a file, a symlink or an empty directory. A
// non-empty directory is ErrExists so a non-recursive delete cannot lose data
// by accident.
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

// RemoveTree deletes rel and everything under it. onEntry, when non-nil, is
// called with each removed entry's root-relative path as it is removed,
// deepest first, so a caller can stream progress. The walk checks ctx before
// every entry so a cancelled Delete stops promptly, and a symlink is removed as
// a link and never followed into its target.
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

// removeTree is the recursive body of RemoveTree.
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

// RemoveStaleTemps deletes every entry under the root whose base name starts
// with prefix and whose modification time is before cutoff, walking each
// directory without following symbolic links. A stale staging directory is
// removed whole (with its children) before the walk would descend into it, so a
// directory a crashed directory copy or a failed staged-copy cleanup left
// behind is reclaimed rather than only its stale children. It returns how many
// top-level entries it removed and joins the errors of entries it could not
// remove, but keeps walking so one stale file does not strand the rest. The
// agent uses it to reclaim staging files a crashed upload left behind.
func (r *Root) RemoveStaleTemps(prefix string, cutoff time.Time) (int, error) {
	if err := r.requireWritable(); err != nil {
		return 0, err
	}
	removed, errs := r.removeStaleTemps(".", prefix, cutoff, 0)
	return removed, errors.Join(errs...)
}

// removeStaleTemps is the recursive body of RemoveStaleTemps. depth bounds the
// descent so the sweep cost stays proportional to a bounded tree.
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
			// A stale staging directory is swept whole: check its prefix and
			// age before descending, so a directory left by a crashed copy (or
			// a failed staged-copy cleanup) is reclaimed even though it has
			// children. A fresh or non-prefixed directory is descended into as
			// before, so a stale file buried inside a live directory is still
			// found.
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

// Copy copies src to dst within the root. A regular file is copied byte for
// byte, a directory recursively and a symlink is recreated with its original
// target text. An existing destination is refused unless overwrite is set;
// overwriting a directory with a file (or the reverse), or a non-empty
// directory, is refused exactly as a same-filesystem rename would be. ctx is
// checked before every entry and between copy chunks.
//
// The copy is staged in a temporary sibling of dst and renamed into place only
// once it is complete, so a failure or cancellation never leaves a partial
// destination tree and an overwrite never deletes the old destination before
// the new one exists. onEntry, when non-nil, is called with each created
// entry's root-relative path as it completes, reported against dst rather than
// the staging name.
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
		// Fail before doing any work when the destination already exists.
		if _, statErr := r.Stat(d); statErr == nil {
			return fmt.Errorf("%w: %s", ErrExists, d)
		} else if !errors.Is(statErr, ErrNotFound) {
			return statErr
		}
	}
	return r.copyStaged(ctx, s, d, overwrite, onEntry)
}

// refuseCopyIntoItself resolves the source and the destination's final parent
// (following symbolic links) and refuses the copy when the destination would
// land on the source itself or inside the source's subtree. Both directions
// matter: a lexically distinct path can resolve onto the source through a
// symlinked parent — "up" -> "." makes "up/f" address "f" — which would
// otherwise turn the staging rename into a self-overwrite that destroys the
// source. An existing destination that is the same inode as the source (a hard
// link or a second name for it) is refused too, so an overwrite never unlinks
// the source.
func (r *Root) refuseCopyIntoItself(s, d string) error {
	resolvedSrc, err := r.Resolve(s)
	if err != nil {
		return err
	}
	resolvedParent, err := r.Resolve(path.Dir(d))
	if err != nil {
		// The destination parent does not exist yet, so the copy cannot land on
		// the source: creating it would fail first.
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

// sameEntry reports whether two root-relative paths name the same underlying
// entry, following symbolic links on both. Device and inode are compared, so a
// hard link or a second path to the same file is detected even when the
// resolved names differ. A path that does not exist is not an error the caller
// acts on: it is reported as (false, nil).
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

// copyStaged copies src into a fresh temporary sibling of dst and renames it
// into place, so a partial copy never becomes the destination and an overwrite
// never removes the old destination first. A failed copy removes the staging
// tree and leaves the destination untouched.
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
		// A non-replacing rename keeps a create exclusive even if the name
		// appears between the check above and the commit.
		err = r.RenameExclusive(stage, dst)
	}
	if err != nil {
		r.removeStaged(stage)
		return mapError("rename", err)
	}
	return nil
}

// removeStaged drops a staging entry after a failed or refused copy. It uses a
// background context so cleanup still runs when the copy was cancelled, and
// ignores errors: the destination is already intact and the sweeper reclaims a
// leak.
func (r *Root) removeStaged(p string) {
	_ = r.RemoveTree(context.Background(), p, nil)
}

// stagedPath maps a path under the staging entry back to the logical
// destination, so streamed progress addresses dst rather than a temporary name.
func stagedPath(stage, dst, rel string) string {
	if rel == stage {
		return dst
	}
	if suffix, ok := strings.CutPrefix(rel, stage+"/"); ok {
		return path.Join(dst, suffix)
	}
	return rel
}

// copyTree copies one entry into a fresh destination path that does not yet
// exist, recursing into directories. Every entry is created exclusively, so a
// staged copy can never clobber an existing entry. depth bounds the recursion
// so a pathological tree is an explicit error, not unbounded recursion.
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
		// Create the directory writable so its children can be copied in even
		// when the source mode is read-only (e.g. 0o555), then set the exact
		// source mode once the children exist. os.Root.Mkdir is masked by the
		// process umask, which would otherwise silently drop group or other
		// bits (e.g. 0o770 -> 0o750); Chmod sets the exact mode as file mode
		// is already set exactly by copyFile.
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

// copyFile streams one regular file to a new destination, creating it
// exclusively and setting the source's mode only after the bytes are in. ctx is
// checked between chunks; on any failure (including cancellation) the partial
// destination is removed so a cancelled copy leaves no temp behind.
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

// copyWithContext copies src to dst, checking ctx between chunks so a large
// file stops promptly when the caller cancels.
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

// dirNames returns every name in a directory. It is used only by copy and
// delete, which must visit the whole subtree anyway.
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

// withinRel reports whether child lies strictly inside the parent subtree of a
// root-relative path, so a copy cannot recurse into its own output.
func withinRel(parent, child string) bool {
	return strings.HasPrefix(child, parent+"/")
}
