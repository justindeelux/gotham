//go:build !linux

package fsroot

import "syscall"

// renameNoReplace reports the no-replace rename primitive as unsupported on
// platforms without renameat2, so the portable fallback runs.
func renameNoReplace(_ int, _ string, _ int, _ string) error { return syscall.ENOSYS }
