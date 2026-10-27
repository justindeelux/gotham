package stats

import (
	"testing"
	"time"
)

// TestFilesystemCollectorRealStatfs exercises the real statfs/device path. A
// temporary directory normally shares the root device, so it must be
// deduplicated into the root entry instead of appearing twice.
func TestFilesystemCollectorRealStatfs(t *testing.T) {
	dir := t.TempDir()
	collector := NewFilesystemCollector()
	filesystems := collector.Collect([]FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: dir, Purpose: PurposeManagedStorage},
	})
	rootDevice, rootErr := fileDevice("/")
	dirDevice, dirErr := fileDevice(dir)
	want := 1
	if rootErr != nil || dirErr != nil || rootDevice != dirDevice {
		want = 2
	}
	if len(filesystems) != want {
		t.Fatalf("filesystems = %+v; want %d after same-device dedupe", filesystems, want)
	}
	if filesystems[0].Path != "/" || filesystems[0].Status != MetricOK {
		t.Errorf("filesystem = %+v; want a measured root", filesystems[0])
	}
	if filesystems[0].TotalBytes == 0 {
		t.Error("root total_bytes = 0; want a measured capacity")
	}

	// A path that cannot be read is an explicit error, never a zero capacity.
	now := time.Now().Add(2 * time.Minute)
	collector.now = func() time.Time { return now }
	missing := collector.Collect([]FilesystemCandidate{{Path: "/does/not/exist/gotham", Purpose: PurposeBackup}})
	if len(missing) != 1 || missing[0].Status != MetricError {
		t.Errorf("missing filesystem = %+v; want an explicit error", missing)
	}
}
