//go:build linux

package fsroot

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// chmodNoFollow applies mode to p's final component without following a
// symbolic link, and without needing read permission on the entry or on its
// parent directories.
//
// It opens the root and every parent component with O_PATH, which needs only
// search (execute) permission on the directories it walks through and no
// permission at all on the final entry, then fstats that descriptor to classify
// the type (a link, special file, hard link or owner-execute-less directory is
// refused) and chmods the same inode. The returned entry is read back from the
// same descriptor, so a parent the agent cannot read does not stop the chmod.
// This makes a 000 or 0o111 entry — one the agent itself just made unreadable —
// recoverable through the product, which an O_RDONLY open could not do for a
// non-root agent.
//
// The chmod acts on the opened inode: a symlink swapped in after the open
// cannot be followed. fchmodat2(AT_EMPTY_PATH) is preferred (Linux 6.6+); on
// older kernels it is unavailable, so the /proc/self/fd magic link — which
// resolves to that same inode — is used instead. Neither needs read permission
// on the entry.
func (r *Root) chmodNoFollow(p string, mode os.FileMode, refuseHardlink bool) (Entry, error) {
	rootFile, err := r.root.OpenFile(".", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return Entry{}, mapError("chmod", err)
	}
	defer func() { _ = rootFile.Close() }()
	rootFd := int(rootFile.Fd())

	dirFd := rootFd
	if parent := path.Dir(p); parent != "." {
		opened, err := openPathDir(rootFd, parent)
		if err != nil {
			return Entry{}, mapError("chmod", err)
		}
		dirFd = opened
	}
	defer func() {
		if dirFd != rootFd {
			_ = unix.Close(dirFd)
		}
	}()

	fd, err := unix.Openat(dirFd, path.Base(p), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return Entry{}, mapError("chmod", err)
	}
	entryFile := os.NewFile(uintptr(fd), p)
	defer func() { _ = entryFile.Close() }()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return Entry{}, mapError("chmod", err)
	}
	target, err := chmodTargetMode(&stat, p, mode, refuseHardlink)
	if err != nil {
		return Entry{}, err
	}
	if err := chmodInode(fd, target); err != nil {
		return Entry{}, mapError("chmod", err)
	}
	info, err := entryFile.Stat()
	if err != nil {
		return Entry{}, mapError("chmod", err)
	}
	return r.entryFor(p, info)
}

// openPathDir opens a root-relative parent directory with O_PATH, so only
// search (execute) permission is needed on it and its ancestors, never read.
// openat2 with RESOLVE_BENEATH resolves intermediate symlinks confined beneath
// the root, matching os.Root; when the kernel or a seccomp filter lacks openat2
// it falls back to a component walk, which refuses a symlinked component rather
// than following it.
func openPathDir(rootFd int, parent string) (int, error) {
	fd, err := unix.Openat2(rootFd, parent, &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err == nil {
		return fd, nil
	}
	switch {
	case errors.Is(err, unix.EXDEV):
		// openat2 reports a path that climbs out of the root as EXDEV.
		return -1, fmt.Errorf("%w: %s escapes the root", ErrEscape, parent)
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL),
		errors.Is(err, unix.EPERM), errors.Is(err, unix.E2BIG):
		return openPathDirWalk(rootFd, parent)
	default:
		return -1, err
	}
}

// openPathDirWalk opens parent one component at a time with O_PATH|O_NOFOLLOW.
func openPathDirWalk(rootFd int, parent string) (int, error) {
	dirFd := rootFd
	for _, comp := range strings.Split(parent, "/") {
		next, err := unix.Openat(dirFd, comp, unix.O_PATH|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			if dirFd != rootFd {
				_ = unix.Close(dirFd)
			}
			return -1, err
		}
		if dirFd != rootFd {
			_ = unix.Close(dirFd)
		}
		dirFd = next
	}
	return dirFd, nil
}

// chmodInode sets mode on the inode behind an O_PATH descriptor.
// fchmodat2(AT_EMPTY_PATH) is the direct primitive; when it is unavailable
// (Linux < 6.6, a seccomp filter, or a capability the kernel wants) the
// /proc/self/fd magic link is followed instead, which reaches the same inode
// without any read permission on the entry.
func chmodInode(fd int, mode uint32) error {
	err := unix.Fchmodat(fd, "", mode, unix.AT_EMPTY_PATH)
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EOPNOTSUPP),
		errors.Is(err, unix.EPERM), errors.Is(err, unix.EINVAL):
		return unix.Chmod("/proc/self/fd/"+strconv.Itoa(fd), mode)
	default:
		return err
	}
}
