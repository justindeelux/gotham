//go:build unix

package updates

import (
	"os"
	"syscall"
)

// withFileLock runs fn while holding an exclusive, process-external advisory
// lock on path. flock serializes both goroutines (each opens its own
// descriptor) and separate processes, so two Applier entry points can never
// interleave their staging/commit steps.
func withFileLock(path string, fn func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }()

	return fn()
}
