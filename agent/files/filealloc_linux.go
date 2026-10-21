//go:build linux

package files

import (
	"os"

	"golang.org/x/sys/unix"
)

// preallocate reserves size bytes on f before an in-place rewrite, so a growing
// file cannot fail with ENOSPC after its old contents were truncated: the
// reservation surfaces an out-of-space failure up front, while the file is
// still intact. On Linux fallocate(2) allocates the blocks; a filesystem that
// does not support it returns EOPNOTSUPP/EINVAL/ENOSYS, which the caller
// ignores and guards with its free-space check instead.
func preallocate(f *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	return unix.Fallocate(int(f.Fd()), 0, 0, size)
}
