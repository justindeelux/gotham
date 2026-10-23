//go:build linux

package fsroot

import "golang.org/x/sys/unix"

func renameNoReplace(oldFd int, oldName string, newFd int, newName string) error {
	return unix.Renameat2(oldFd, oldName, newFd, newName, unix.RENAME_NOREPLACE)
}
