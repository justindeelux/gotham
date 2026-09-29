//go:build !unix

package updates

import "errors"

// withFileLock is unavailable off Unix; the control plane targets linux/amd64
// and linux/arm64, and tests run on darwin.
func withFileLock(_ string, _ func() error) error {
	return errors.New("updates: file locking is not supported on this platform")
}

// tryFileLock is unavailable off Unix.
func tryFileLock(_ string, _ func() error) (bool, error) {
	return false, errors.New("updates: file locking is not supported on this platform")
}
