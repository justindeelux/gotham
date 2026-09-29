//go:build darwin

package stats

import (
	"errors"
	"testing"
)

// TestIOUnsupportedOnDarwin pins the documented ceiling: macOS exposes its
// interface and disk counters only through Mach APIs golang.org/x/sys/unix
// does not wrap, so the agent reports zero I/O rates instead of failing.
func TestIOUnsupportedOnDarwin(t *testing.T) {
	if _, _, err := readNetwork(); !errors.Is(err, errUnsupported) {
		t.Errorf("readNetwork = %v; want errUnsupported", err)
	}
	if _, _, err := readDiskIO(); !errors.Is(err, errUnsupported) {
		t.Errorf("readDiskIO = %v; want errUnsupported", err)
	}
}
