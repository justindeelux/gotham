//go:build linux

package stats

import (
	"testing"
	"time"
)

func TestFilesystemCollectorExcludesPseudoFilesystems(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	values := map[string]fakeCapacity{
		"/":     {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
		"/proc": {capacity: capacity{totalBytes: 1, availableBytes: 1, totalInodes: 1, freeInodes: 1, kind: magicProc}},
	}
	collector := newTestFilesystemCollector(&now, values, map[string]uint64{"/": 7, "/proc": 8})
	filesystems := collector.Collect([]FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: "/proc", Purpose: PurposeBackup},
	})
	if len(filesystems) != 1 || filesystems[0].Path != "/" {
		t.Fatalf("filesystems = %+v; want only the root", filesystems)
	}
}
