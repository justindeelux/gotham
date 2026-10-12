//go:build linux

package fsroot

import "golang.org/x/sys/unix"

// renameNoReplace performs renameat2 with RENAME_NOREPLACE relative to the two
// directory descriptors, so an existing destination is refused atomically. A
// filesystem or kernel without the primitive surfaces ENOSYS/EINVAL/EOPNOTSUPP
// for the caller's portable fallback.
func renameNoReplace(oldFd int, oldName string, newFd int, newName string) error {
	return unix.Renameat2(oldFd, oldName, newFd, newName, unix.RENAME_NOREPLACE)
}
