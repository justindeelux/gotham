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
		return -1, fmt.Errorf("%w: %s escapes the root", ErrEscape, parent)
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL),
		errors.Is(err, unix.EPERM), errors.Is(err, unix.E2BIG):
		return openPathDirWalk(rootFd, parent)
	default:
		return -1, err
	}
}

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
