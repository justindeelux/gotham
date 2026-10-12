package fsroot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// fakeDirEntry lets a test drive the page builder with an entry whose stat
// fails, standing in for a file deleted between the directory read and Info.
type fakeDirEntry struct {
	name string
	info os.FileInfo
	err  error
}

func (d fakeDirEntry) Name() string               { return d.name }
func (d fakeDirEntry) IsDir() bool                { return false }
func (d fakeDirEntry) Type() fs.FileMode          { return 0 }
func (d fakeDirEntry) Info() (os.FileInfo, error) { return d.info, d.err }

type fakeFileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// TestPageSkipsVanishedEntry pins that one entry that vanishes between the
// directory read and its stat is skipped instead of aborting the page.
func TestPageSkipsVanishedEntry(t *testing.T) {
	root, err := Open("t", t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	dirs := []os.DirEntry{
		fakeDirEntry{name: "gone", err: &os.PathError{Op: "lstat", Path: "gone", Err: syscall.ENOENT}},
		fakeDirEntry{name: "here", info: fakeFileInfo{name: "here", size: 3, mode: 0o644}},
	}
	entries, err := root.pageFromDirs(".", dirs)
	if err != nil {
		t.Fatalf("pageFromDirs: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "here" {
		t.Fatalf("entries = %+v, want just %q", entries, "here")
	}
}

// TestMapErrorCodes pins the error vocabulary, including the escape
// classification so a change to the stdlib message is caught here rather than
// surfacing as an Internal error.
func TestMapErrorCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"symlink escape", &os.PathError{Op: "open", Path: "x", Err: errors.New("path escapes from parent")}, ErrEscape},
		{"symlink loop", &os.PathError{Op: "open", Path: "x", Err: syscall.ELOOP}, ErrInvalid},
		{"not a directory", &os.PathError{Op: "open", Path: "x", Err: syscall.ENOTDIR}, ErrInvalid},
		{"missing", &os.PathError{Op: "open", Path: "x", Err: syscall.ENOENT}, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapError("open", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("mapError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestListBoundedAllocation proves a directory far larger than the scan cap
// does not allocate memory proportional to its size: only the capped number of
// entries is ever read. Without the cap (the old ReadDir(-1)) this directory
// allocates well over a megabyte.
func TestListBoundedAllocation(t *testing.T) {
	oldBatch, oldCap := listBatchSize, listScanCap
	listBatchSize, listScanCap = 64, 64
	t.Cleanup(func() { listBatchSize, listScanCap = oldBatch, oldCap })

	dir := t.TempDir()
	const total = 3000
	for i := range total {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := Open("t", dir, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	entries, truncated, _, err := root.List(".", "", 10)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 10 || !truncated {
		t.Fatalf("page = %d entries, truncated=%v, want 10/true", len(entries), truncated)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 256<<10 {
		t.Fatalf("list allocated %d bytes over a %d-entry directory; want a bounded scan", grew, total)
	}
}
