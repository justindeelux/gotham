//go:build linux

package stats

import "testing"

// TestReadIOFromProc verifies the real /proc and /sys reads on the production
// target: both hooks must answer without error and report a device set that
// excludes the loop and ram pseudo devices.
func TestReadIOFromProc(t *testing.T) {
	if _, _, err := readNetwork(); err != nil {
		t.Errorf("readNetwork: %v", err)
	}
	if _, _, err := readDiskIO(); err != nil {
		t.Errorf("readDiskIO: %v", err)
	}

	devices, err := wholeBlockDevices()
	if err != nil {
		t.Fatalf("wholeBlockDevices: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("wholeBlockDevices() = empty; want at least one device")
	}
	for _, name := range []string{"loop0", "ram0", "zram0"} {
		if _, ok := devices[name]; ok {
			t.Errorf("wholeBlockDevices() includes pseudo device %q", name)
		}
	}
}
