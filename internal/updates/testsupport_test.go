package updates

import (
	"context"
	"os"
	"testing"
)

// noopRestart is a RestartFunc that reports the wrapper exited cleanly. The
// control-plane tests wire it where the real sudo wrapper must not run.
func noopRestart(context.Context) (func() error, error) {
	return func() error { return nil }, nil
}

// readFile reads a path, failing the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
