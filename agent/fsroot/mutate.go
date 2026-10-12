package fsroot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
)

// tempNameTries bounds how many random temporary names CreateTemp tries before
// giving up; a collision this deep would mean something is very wrong.
const tempNameTries = 100

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
	return path.Join(dir, ".gotham-tmp-"+hex.EncodeToString(suffix[:])), nil
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
// freshly created temporary file the requested or inherited mode.
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
// deepest first, so a caller can stream progress; a symlink is removed as a
// link and never followed into its target.
func (r *Root) RemoveTree(rel string, onEntry func(string, Entry) error) error {
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
	return r.removeTree(p, onEntry)
}

// removeTree is the recursive body of RemoveTree.
func (r *Root) removeTree(p string, onEntry func(string, Entry) error) error {
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
			if err := r.removeTree(path.Join(p, name), onEntry); err != nil {
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

// Copy copies src to dst within the root. A regular file is copied byte for
// byte, a directory recursively and a symlink is recreated with its original
// target text. An existing destination is refused unless overwrite is set;
// overwriting a directory with a file (or the reverse) is always refused.
// onEntry, when non-nil, is called with each created entry's root-relative
// path as it completes.
func (r *Root) Copy(src, dst string, overwrite bool, onEntry func(string, Entry) error) error {
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
	return r.copyEntry(s, d, overwrite, onEntry)
}

// copyEntry copies one entry, recursing into a directory.
func (r *Root) copyEntry(src, dst string, overwrite bool, onEntry func(string, Entry) error) error {
	entry, err := r.Stat(src)
	if err != nil {
		return err
	}
	dstEntry, dstErr := r.Stat(dst)
	dstExists := dstErr == nil
	if dstErr != nil && !errors.Is(dstErr, ErrNotFound) {
		return dstErr
	}
	if dstExists && !overwrite {
		return fmt.Errorf("%w: %s", ErrExists, dst)
	}

	switch entry.Kind {
	case KindDirectory:
		if dstExists && dstEntry.Kind != KindDirectory {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
		if !dstExists {
			if err := r.root.Mkdir(dst, entry.Mode.Perm()); err != nil {
				return mapError("mkdir", err)
			}
		}
		names, err := r.dirNames(src)
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := r.copyEntry(path.Join(src, name), path.Join(dst, name), overwrite, onEntry); err != nil {
				return err
			}
		}
	case KindSymlink:
		if err := r.prepareCopyTarget(dst, dstExists, dstEntry); err != nil {
			return err
		}
		if err := r.root.Symlink(entry.SymlinkTarget, dst); err != nil {
			return mapError("symlink", err)
		}
	case KindRegular:
		if err := r.prepareCopyTarget(dst, dstExists, dstEntry); err != nil {
			return err
		}
		if err := r.copyFile(src, dst, entry.Mode.Perm()); err != nil {
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

// prepareCopyTarget clears an existing non-directory destination so a fresh
// entry can be created in its place. The overwrite policy is enforced by the
// caller before this point.
func (r *Root) prepareCopyTarget(dst string, dstExists bool, dstEntry Entry) error {
	if !dstExists {
		return nil
	}
	if dstEntry.Kind == KindDirectory {
		return fmt.Errorf("%w: %s", ErrExists, dst)
	}
	if err := r.root.Remove(dst); err != nil {
		return mapError("remove", err)
	}
	return nil
}

// copyFile streams one regular file to a new destination, creating it
// exclusively and setting the source's mode only after the bytes are in.
func (r *Root) copyFile(src, dst string, mode os.FileMode) error {
	in, _, err := r.OpenRegular(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := r.root.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return mapError("create", err)
	}
	if _, err := io.Copy(out, in); err != nil {
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
