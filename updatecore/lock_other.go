//go:build !unix

package updatecore

import "errors"

func withFileLock(_ string, _ func() error) error {
	return errors.New("updates: file locking is not supported on this platform")
}

func tryFileLock(_ string, _ func() error) (bool, error) {
	return false, errors.New("updates: file locking is not supported on this platform")
}
