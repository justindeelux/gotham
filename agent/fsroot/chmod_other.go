//go:build !linux

package fsroot

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
)

// chmodNoFollow applies mode to p's final component without following a
// symbolic link, using an O_RDONLY open (the portable path). Unlike the Linux
// implementation it needs read permission on the entry and its parents, so a
// 000 entry is not recoverable here; the agent only ships on Linux, and this
// path exists so the package still builds and its tests run on the development
// host. O_NOCTTY keeps a device node from becoming the controlling terminal.
func (r *Root) chmodNoFollow(p string, mode os.FileMode, refuseHardlink bool) (Entry, error) {
	parent, err := r.root.OpenRoot(path.Dir(p))
	if err != nil {
		return Entry{}, mapError("chmod", err)
	}
	defer func() { _ = parent.Close() }()
	dirFile, err := openDirFd(parent)
	if err != nil {
		return Entry{}, mapError("chmod", err)
	}
	defer func() { _ = dirFile.Close() }()
	fd, err := unix.Openat(int(dirFile.Fd()), path.Base(p), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
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
	if err := unix.Fchmod(fd, target); err != nil {
		return Entry{}, mapError("chmod", err)
	}
	info, err := entryFile.Stat()
	if err != nil {
		return Entry{}, mapError("chmod", err)
	}
	return r.entryFor(p, info)
}
