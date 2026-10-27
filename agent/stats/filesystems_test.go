package stats

import (
	"errors"
	"io/fs"
	"testing"
	"time"
)

type fakeCapacity struct {
	capacity
	err error
}

func newTestFilesystemCollector(now *time.Time, values map[string]fakeCapacity, devices map[string]uint64) *FilesystemCollector {
	return &FilesystemCollector{
		now:    func() time.Time { return *now },
		maxAge: time.Minute,
		capacity: func(path string) (capacity, error) {
			value, ok := values[path]
			if !ok {
				return capacity{}, errors.New("missing path")
			}
			return value.capacity, value.err
		},
		device: func(path string) (uint64, error) {
			device, ok := devices[path]
			if !ok {
				return 0, errors.New("missing device")
			}
			return device, nil
		},
	}
}

func TestFilesystemCollectorDedupesAndExcludesPseudo(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	values := map[string]fakeCapacity{
		"/":                       {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
		"/var/lib/gotham/volumes": {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
		"/mnt/backup":             {capacity: capacity{totalBytes: 200, availableBytes: 10, totalInodes: 20, freeInodes: 2, kind: 2}},
	}
	devices := map[string]uint64{
		"/":                       7,
		"/var/lib/gotham/volumes": 7,
		"/mnt/backup":             8,
	}
	collector := newTestFilesystemCollector(&now, values, devices)
	candidates := []FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: "/var/lib/gotham/volumes", Purpose: PurposeManagedStorage},
		{Path: "/mnt/backup", Purpose: PurposeBackup},
	}
	filesystems := collector.Collect(candidates)
	if len(filesystems) != 2 {
		t.Fatalf("filesystems = %+v; want root and the separate backup mount", filesystems)
	}
	if filesystems[0].Path != "/" || filesystems[0].Purpose != PurposeRoot {
		t.Errorf("first filesystem = %+v; want the root", filesystems[0])
	}
	if filesystems[1].Path != "/mnt/backup" || filesystems[1].Purpose != PurposeBackup {
		t.Errorf("second filesystem = %+v; want the independent backup mount", filesystems[1])
	}
	if filesystems[1].TotalBytes != 200 || filesystems[1].AvailableBytes != 10 {
		t.Errorf("backup capacity = %+v", filesystems[1])
	}
}

func TestFilesystemCollectorRefreshesTopologyAtMostOncePerWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	calls := 0
	values := map[string]fakeCapacity{
		"/": {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
	}
	collector := newTestFilesystemCollector(&now, values, map[string]uint64{"/": 7})
	probe := collector.capacity
	collector.capacity = func(path string) (capacity, error) {
		calls++
		return probe(path)
	}
	collector.Collect([]FilesystemCandidate{{Path: "/", Purpose: PurposeRoot}})
	afterFirst := calls
	now = now.Add(30 * time.Second)
	collector.Collect([]FilesystemCandidate{{Path: "/", Purpose: PurposeRoot}})
	withinWindow := calls - afterFirst
	if withinWindow != 1 {
		t.Errorf("capacity reads within the window = %d; want one per sample", withinWindow)
	}
	now = now.Add(31 * time.Second)
	afterRefresh := calls
	collector.Collect([]FilesystemCandidate{{Path: "/", Purpose: PurposeRoot}})
	if calls-afterRefresh < 2 {
		t.Errorf("capacity reads after the window = %d; want a topology refresh plus a sample", calls-afterRefresh)
	}
}

func TestFilesystemCollectorOmitsMissingCandidate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	values := map[string]fakeCapacity{
		"/":                       {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
		"/var/lib/gotham/volumes": {err: &fs.PathError{Op: "statfs", Path: "/var/lib/gotham/volumes", Err: fs.ErrNotExist}},
		"/same-device":            {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
		"/mnt/backup":             {capacity: capacity{totalBytes: 200, availableBytes: 10, totalInodes: 20, freeInodes: 2, kind: 2}},
	}
	devices := map[string]uint64{
		"/": 7, "/same-device": 7, "/mnt/backup": 8,
	}
	collector := newTestFilesystemCollector(&now, values, devices)
	filesystems := collector.Collect([]FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: "/var/lib/gotham/volumes", Purpose: PurposeManagedStorage},
		{Path: "/same-device", Purpose: PurposeDockerData},
		{Path: "/mnt/backup", Purpose: PurposeBackup},
	})
	if len(filesystems) != 2 {
		t.Fatalf("filesystems = %+v; want root and the separate backup mount only", filesystems)
	}
	if filesystems[0].Path != "/" || filesystems[0].Status != MetricOK {
		t.Errorf("root = %+v; want it reported", filesystems[0])
	}
	if filesystems[1].Path != "/mnt/backup" || filesystems[1].Status != MetricOK {
		t.Errorf("backup = %+v; want the existing separate mount reported", filesystems[1])
	}
	for _, filesystem := range filesystems {
		if filesystem.Path == "/var/lib/gotham/volumes" {
			t.Errorf("a not-yet-created path was reported: %+v", filesystem)
		}
	}
}

func TestFilesystemCollectorReportsPermissionFailure(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	values := map[string]fakeCapacity{
		"/":          {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}},
		"/forbidden": {err: &fs.PathError{Op: "statfs", Path: "/forbidden", Err: fs.ErrPermission}},
	}
	collector := newTestFilesystemCollector(&now, values, map[string]uint64{"/": 7, "/forbidden": 8})
	filesystems := collector.Collect([]FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: "/forbidden", Purpose: PurposeBackup},
	})
	if len(filesystems) != 2 {
		t.Fatalf("filesystems = %+v; want the readable root and the failed candidate", filesystems)
	}
	failed := filesystems[1]
	if failed.Status != MetricError || failed.Reason != "read_failed" ||
		failed.InodeStatus != MetricError || failed.TotalBytes != 0 {
		t.Errorf("permission failure = %+v; want error/read_failed, never a zero measurement", failed)
	}
}

func TestFilesystemCollectorOmitsPathThatDisappears(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	calls := 0
	collector := &FilesystemCollector{
		now:    func() time.Time { return now },
		maxAge: time.Minute,
		capacity: func(path string) (capacity, error) {
			if path == "/gone" {
				calls++
				if calls == 1 {
					return capacity{totalBytes: 10, availableBytes: 5, totalInodes: 1, freeInodes: 1, kind: 1}, nil
				}
				return capacity{}, fs.ErrNotExist
			}
			return capacity{totalBytes: 100, availableBytes: 40, totalInodes: 10, freeInodes: 4, kind: 1}, nil
		},
		device: func(path string) (uint64, error) {
			if path == "/gone" {
				return 8, nil
			}
			return 7, nil
		},
	}
	filesystems := collector.Collect([]FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: "/gone", Purpose: PurposeBackup},
	})
	if len(filesystems) != 1 || filesystems[0].Path != "/" {
		t.Errorf("filesystems = %+v; a path that disappeared must be omitted, not failed", filesystems)
	}
}

func TestFilesystemCollectorReportsFailureAndUnknownInodes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	values := map[string]fakeCapacity{
		"/":     {capacity: capacity{totalBytes: 100, availableBytes: 40, totalInodes: 0, freeInodes: 0, kind: 1}},
		"/lost": {err: errors.New("statfs failed")},
	}
	collector := newTestFilesystemCollector(&now, values, map[string]uint64{"/": 7, "/lost": 8})
	filesystems := collector.Collect([]FilesystemCandidate{
		{Path: "/", Purpose: PurposeRoot},
		{Path: "/lost", Purpose: PurposeBackup},
	})
	if len(filesystems) != 2 {
		t.Fatalf("filesystems = %+v; want both candidates", filesystems)
	}
	if filesystems[0].Status != MetricOK {
		t.Errorf("root status = %s; want ok", filesystems[0].Status)
	}
	if filesystems[0].InodeStatus != MetricUnsupported || filesystems[0].TotalInodes != 0 {
		t.Errorf("root inode evidence = %+v; want explicit unsupported", filesystems[0])
	}
	if filesystems[1].Status != MetricError || filesystems[1].Reason != "read_failed" {
		t.Errorf("failed filesystem = %+v; want an explicit error, not zero", filesystems[1])
	}
}
