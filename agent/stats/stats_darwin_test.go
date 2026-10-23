//go:build darwin

package stats

import (
	"errors"
	"testing"
)

func TestIOUnsupportedOnDarwin(t *testing.T) {
	if _, _, err := readNetwork(); !errors.Is(err, errUnsupported) {
		t.Errorf("readNetwork = %v; want errUnsupported", err)
	}
	if _, _, err := readDiskIO(); !errors.Is(err, errUnsupported) {
		t.Errorf("readDiskIO = %v; want errUnsupported", err)
	}
}
