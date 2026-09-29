//go:build unix

package updates

import (
	"errors"
	"os"
	"syscall"
)

// withFileLock runs fn while holding an exclusive, process-external advisory
// lock on path. flock serializes both goroutines (each opens its own
// descriptor) and separate processes, so two Applier entry points — including
// the privileged wrapper — can never interleave their staging/commit steps.
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

// tryFileLock runs fn while holding the lock when it is free, without blocking.
// It reports whether the lock was acquired; a held lock is not an error, so
// startup recovery can skip while the privileged wrapper owns the transaction.
func tryFileLock(path string, fn func() error) (bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	defer file.Close()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }()

	return true, fn()
}
