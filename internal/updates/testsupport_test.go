package updates

import (
	"context"
	"os"
	"testing"
)

func noopRestart(context.Context) (func() error, error) {
	return func() error { return nil }, nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
