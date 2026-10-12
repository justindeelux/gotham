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

// openInternalRoot opens a temp directory as a root for an internal test.
func openInternalRoot(t *testing.T, dir string) *Root {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, err := Open("t", dir, info, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// TestPageSkipsVanishedEntry pins that one entry that vanishes between the
// directory read and its stat is skipped instead of aborting the page.
func TestPageSkipsVanishedEntry(t *testing.T) {
	root := openInternalRoot(t, t.TempDir())

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

// TestListAllEntriesVanishAdvancesCursor pins the loop fix: when every entry
// of a truncated page vanishes between ReadDir and its stat, the page is empty
// but the cursor must still advance to the last directory name seen, not to
// the last surviving entry (which does not exist).
func TestListAllEntriesVanishAdvancesCursor(t *testing.T) {
	root := openInternalRoot(t, t.TempDir())

	dirs := []os.DirEntry{
		fakeDirEntry{name: "a", err: &os.PathError{Op: "lstat", Path: "a", Err: syscall.ENOENT}},
		fakeDirEntry{name: "b", err: &os.PathError{Op: "lstat", Path: "b", Err: syscall.ENOENT}},
		fakeDirEntry{name: "c", info: fakeFileInfo{name: "c", size: 1, mode: 0o644}},
	}
	page, truncated, last := selectPage(dirs, 2)
	if !truncated || last != "b" {
		t.Fatalf("selectPage truncated=%v last=%q, want true/%q", truncated, last, "b")
	}
	entries, err := root.pageFromDirs(".", page)
	if err != nil {
		t.Fatalf("pageFromDirs: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none (both vanished)", entries)
	}
}

// TestListScanCapIsExplicitError proves a directory larger than the scan cap
// is refused, never returned as a silently partial page that would strand a
// client in a paging loop.
func TestListScanCapIsExplicitError(t *testing.T) {
	old := listScanCap
	listScanCap = 4
	t.Cleanup(func() { listScanCap = old })

	dir := t.TempDir()
	for i := range 10 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := openInternalRoot(t, dir)
	if _, _, _, err := root.List(".", "", 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("List over the cap = %v, want ErrTooLarge", err)
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

// TestListBoundedRetainedHeap proves a directory far larger than the page does
// not retain memory proportional to its size: the page is limit entries and
// the live heap after the call holds only the page, not the directory. The
// streaming batches are garbage, so a regression that buffered the whole
// directory would show up as a live heap on the order of the entry count.
func TestListBoundedRetainedHeap(t *testing.T) {
	oldBatch := listBatchSize
	listBatchSize = 64
	t.Cleanup(func() { listBatchSize = oldBatch })

	dir := t.TempDir()
	const total = 20_000
	for i := range total {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := openInternalRoot(t, dir)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	entries, truncated, _, err := root.List(".", "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if len(entries) != 10 || !truncated {
		t.Fatalf("page = %d entries, truncated=%v, want 10/true", len(entries), truncated)
	}
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 1<<20 {
		t.Fatalf("list retained %d live bytes over a %d-entry directory; want a bounded heap", grew, total)
	}
}
