//go:build !linux

package fsroot

import "syscall"

func renameNoReplace(_ int, _ string, _ int, _ string) error { return syscall.ENOSYS }
