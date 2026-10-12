package fsroot_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/justindeelux/gotham/agent/fsroot"
)

func openRoot(t *testing.T, dir string) *fsroot.Root {
	t.Helper()
	root, err := fsroot.Open("test", dir, true)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestOpenRequiresAbsolutePath(t *testing.T) {
	t.Parallel()
	if _, err := fsroot.Open("test", "relative/path", true); err == nil {
		t.Fatal("expected an error for a relative root")
	}
}

func TestListAndStat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	entries, truncated, cursor, err := root.List(".", "", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if truncated || cursor != "" {
		t.Fatalf("truncated=%v cursor=%q, want false/empty", truncated, cursor)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Name != "a.txt" || entries[0].Kind != fsroot.KindRegular {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if entries[1].Name != "sub" || entries[1].Kind != fsroot.KindDirectory {
		t.Fatalf("second entry = %+v", entries[1])
	}
	if entries[0].Etag == "" {
		t.Fatal("etag is empty")
	}

	entry, err := root.Stat("a.txt")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if entry.Name != "a.txt" || entry.Size != 5 || entry.Mode != 0o640 {
		t.Fatalf("stat entry = %+v", entry)
	}
	if entry.Etag != entries[0].Etag {
		t.Fatalf("stat etag %q != list etag %q", entry.Etag, entries[0].Etag)
	}
}

func TestListPagination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 10 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root := openRoot(t, dir)

	first, truncated, cursor, err := root.List(".", "", 4)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if !truncated || cursor != "f03" || len(first) != 4 {
		t.Fatalf("page 1 = %d entries, truncated=%v cursor=%q", len(first), truncated, cursor)
	}
	second, truncated, cursor, err := root.List(".", cursor, 4)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if !truncated || cursor != "f07" || len(second) != 4 {
		t.Fatalf("page 2 = %d entries, truncated=%v cursor=%q", len(second), truncated, cursor)
	}
	third, truncated, cursor, err := root.List(".", cursor, 4)
	if err != nil {
		t.Fatalf("list page 3: %v", err)
	}
	if truncated || cursor != "" || len(third) != 2 {
		t.Fatalf("page 3 = %d entries, truncated=%v cursor=%q", len(third), truncated, cursor)
	}
}

func TestTraversalRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := openRoot(t, dir)

	for _, rel := range []string{"..", "../etc", "a/../../etc", "/etc", string([]byte{'a', 0, 'b'})} {
		if _, err := root.Stat(rel); !errors.Is(err, fsroot.ErrInvalid) {
			t.Errorf("Stat(%q) error = %v, want ErrInvalid", rel, err)
		}
		if _, _, _, err := root.List(rel, "", 10); !errors.Is(err, fsroot.ErrInvalid) {
			t.Errorf("List(%q) error = %v, want ErrInvalid", rel, err)
		}
	}
}

func TestSymlinkEscape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "linkfile")); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	// A symlink is listed, not followed: its kind is symlink and the escape
	// has not been performed by listing it.
	entries, _, _, err := root.List(".", "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.Kind != fsroot.KindSymlink || entry.SymlinkTarget == "" {
			t.Fatalf("entry %+v is not a reported symlink", entry)
		}
	}

	// Opening or walking through the link must fail closed.
	if _, _, err := root.OpenRegular("linkfile"); !errors.Is(err, fsroot.ErrEscape) {
		t.Errorf("OpenRegular(linkfile) error = %v, want ErrEscape", err)
	}
	if _, _, _, err := root.List("linkdir", "", 10); !errors.Is(err, fsroot.ErrEscape) {
		t.Errorf("List(linkdir) error = %v, want ErrEscape", err)
	}
	if _, err := root.Stat("linkdir/missing"); !errors.Is(err, fsroot.ErrEscape) {
		t.Errorf("Stat(linkdir/missing) error = %v, want ErrEscape", err)
	}
}

func TestSymlinkSwap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dir, "dir")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "inside"), []byte("i"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if _, _, _, err := root.List("dir", "", 10); err != nil {
		t.Fatalf("list before swap: %v", err)
	}

	// Swap the real directory for an escaping symlink between operations.
	if err := os.RemoveAll(inner); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, inner); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := root.List("dir", "", 10); !errors.Is(err, fsroot.ErrEscape) {
		t.Errorf("List after swap error = %v, want ErrEscape", err)
	}
	if _, _, err := root.OpenRegular("dir/secret"); !errors.Is(err, fsroot.ErrEscape) {
		t.Errorf("OpenRegular after swap error = %v, want ErrEscape", err)
	}
}

func TestSpecialFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	socket := filepath.Join(dir, "sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = listener.Close() }()

	root := openRoot(t, dir)
	entries, _, _, err := root.List(".", "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	kinds := map[string]fsroot.Kind{}
	for _, entry := range entries {
		kinds[entry.Name] = entry.Kind
	}
	if kinds["pipe"] != fsroot.KindSpecial {
		t.Errorf("fifo kind = %v, want KindSpecial", kinds["pipe"])
	}
	if kinds["sock"] != fsroot.KindSpecial {
		t.Errorf("socket kind = %v, want KindSpecial", kinds["sock"])
	}

	// A FIFO must never be opened for streaming: O_NONBLOCK returns at once
	// and the type check refuses it.
	if _, _, err := root.OpenRegular("pipe"); !errors.Is(err, fsroot.ErrSpecial) {
		t.Errorf("OpenRegular(pipe) error = %v, want ErrSpecial", err)
	}
	if _, _, err := root.OpenRegular("sock"); err == nil {
		t.Error("OpenRegular(sock) succeeded, want an error")
	}

	if os.Geteuid() != 0 {
		return
	}
	// A character device exercises the fstat path: open succeeds, the type
	// check refuses it.
	dev := filepath.Join(dir, "null")
	if err := syscall.Mknod(dev, syscall.S_IFCHR|0o600, int(unixMkdev(1, 3))); err != nil {
		t.Fatalf("mknod: %v", err)
	}
	if _, _, err := root.OpenRegular("null"); !errors.Is(err, fsroot.ErrSpecial) {
		t.Errorf("OpenRegular(null) error = %v, want ErrSpecial", err)
	}
}

func TestUnreadableFile(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0o000 file")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "locked")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)
	if _, _, err := root.OpenRegular("locked"); !errors.Is(err, fsroot.ErrPermission) {
		t.Fatalf("OpenRegular error = %v, want ErrPermission", err)
	}
}

func TestStatMissing(t *testing.T) {
	t.Parallel()
	root := openRoot(t, t.TempDir())
	if _, err := root.Stat("nope"); !errors.Is(err, fsroot.ErrNotFound) {
		t.Fatalf("Stat error = %v, want ErrNotFound", err)
	}
}

// unixMkdev packs a device number the same way Linux' makedev does, so a char
// device can be created root-only in the special-file test.
func unixMkdev(major, minor uint32) uint64 {
	return (uint64(major) << 8) | uint64(minor)
}
