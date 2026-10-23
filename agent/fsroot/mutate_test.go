package fsroot_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/justindeelux/gotham/agent/fsroot"
)

func TestRenameExclusiveRefusesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if err := root.RenameExclusive("a", "b"); !errors.Is(err, fsroot.ErrExists) {
		t.Fatalf("RenameExclusive onto an existing file = %v, want ErrExists", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b")); string(got) != "b" {
		t.Fatalf("b = %q, want it unchanged", got)
	}
	if err := root.RenameExclusive("a", "d"); !errors.Is(err, fsroot.ErrExists) {
		t.Fatalf("RenameExclusive onto an existing dir = %v, want ErrExists", err)
	}
	if err := root.RenameExclusive("a", "c"); err != nil {
		t.Fatalf("RenameExclusive to a free name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "c")); string(got) != "a" {
		t.Fatalf("c = %q, want %q", got, "a")
	}
}

func TestResolveSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(dir, "a", "up")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "esc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop", filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if got, err := root.Resolve("link/sub"); err != nil || got != "real/sub" {
		t.Fatalf("Resolve(link/sub) = %q, %v; want real/sub", got, err)
	}
	if got, err := root.Resolve("a/up/real"); err != nil || got != "real" {
		t.Fatalf("Resolve(a/up/real) = %q, %v; want real", got, err)
	}
	if _, err := root.Resolve("esc"); !errors.Is(err, fsroot.ErrEscape) {
		t.Fatalf("Resolve(esc) = %v, want ErrEscape", err)
	}
	if _, err := root.Resolve("loop"); !errors.Is(err, fsroot.ErrEscape) {
		t.Fatalf("Resolve(loop) = %v, want ErrEscape", err)
	}
	if _, err := root.Resolve("../x"); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("Resolve(../x) = %v, want ErrInvalid", err)
	}
}

func TestCopyRefusesSymlinkedSelfCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src", filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src/sub", filepath.Join(dir, "deep")); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if err := root.Copy(context.Background(), "src", "out/inner", true, nil); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("copy into a symlink to the source = %v, want ErrInvalid", err)
	}
	if err := root.Copy(context.Background(), "src", "deep/inner", true, nil); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("copy into a symlink inside the source = %v, want ErrInvalid", err)
	}
	if err := root.Copy(context.Background(), "src", "dst", false, nil); err != nil {
		t.Fatalf("normal copy: %v", err)
	}
}

func TestCopyHonoursContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.Copy(ctx, "src", "dst", false, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Copy with a cancelled context = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dst")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a cancelled copy created its destination: %v", err)
	}
	if err := root.RemoveTree(ctx, "src", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveTree with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestCopyRefusesCopyOntoItselfThroughSymlinkedParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "A", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if err := root.Copy(context.Background(), "f", "up/f", true, nil); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("file copy onto itself through a symlink = %v, want ErrInvalid", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "f")); err != nil || string(got) != "payload" {
		t.Fatalf("the source file was destroyed: %q, %v", got, err)
	}
	if err := root.Copy(context.Background(), "A", "up/A", true, nil); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("directory copy onto itself through a symlink = %v, want ErrInvalid", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "A", "a.txt")); err != nil {
		t.Fatalf("the source directory was destroyed: %v", err)
	}
	if err := root.Copy(context.Background(), "f", "up/g", true, nil); err != nil {
		t.Fatalf("legitimate copy through the link: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "g")); err != nil || string(got) != "payload" {
		t.Fatalf("copied file = %q, %v", got, err)
	}
}

func TestCopyFailureLeavesDestinationIntact(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, sub := range []string{"src", "dst"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, "src", name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "dst", "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	copies := 0
	onEntry := func(string, fsroot.Entry) error {
		copies++
		if copies == 1 {
			cancel()
		}
		return nil
	}
	if err := root.Copy(ctx, "src", "dst", true, onEntry); !errors.Is(err, context.Canceled) {
		t.Fatalf("copy aborted mid-walk = %v, want context.Canceled", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "dst"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "old" {
		t.Fatalf("a failed copy modified the destination: %v", entries)
	}
	leftover, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range leftover {
		if strings.HasPrefix(e.Name(), fsroot.TempPrefix) {
			t.Fatalf("a staging entry leaked: %s", e.Name())
		}
	}
}

func TestRemoveStaleTemps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "precious")
	if err := os.WriteFile(outsideFile, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)

	oldTemp := filepath.Join(dir, fsroot.TempPrefix+"old")
	if err := os.WriteFile(oldTemp, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldTemp, old, old); err != nil {
		t.Fatal(err)
	}
	freshTemp := filepath.Join(dir, fsroot.TempPrefix+"new")
	if err := os.WriteFile(freshTemp, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	subOld := filepath.Join(dir, "sub", fsroot.TempPrefix+"old")
	if err := os.WriteFile(subOld, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(subOld, old, old); err != nil {
		t.Fatal(err)
	}
	outsideTemp := filepath.Join(outside, fsroot.TempPrefix+"hidden")
	if err := os.WriteFile(outsideTemp, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outsideTemp, old, old); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, fsroot.TempPrefix+"link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := unix.Lutimes(link, []unix.Timeval{
		unix.NsecToTimeval(old.UnixNano()),
		unix.NsecToTimeval(old.UnixNano()),
	}); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	removed, err := root.RemoveStaleTemps(fsroot.TempPrefix, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("RemoveStaleTemps: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	if _, err := os.Stat(freshTemp); err != nil {
		t.Fatalf("a fresh staging file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("an ordinary file was removed: %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stale symlink was not removed: %v", err)
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("the sweep followed a symlink and touched the target: %v", err)
	}
	if _, err := os.Stat(outsideTemp); err != nil {
		t.Fatalf("the sweep followed a symlink into the target directory: %v", err)
	}
}

func TestRemoveStaleTempsSweepsStaleDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)

	stale := filepath.Join(dir, fsroot.TempPrefix+"abc")
	if err := os.MkdirAll(filepath.Join(stale, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "child", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(dir, fsroot.TempPrefix+"fresh")
	if err := os.Mkdir(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fresh, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ordinary := filepath.Join(dir, "ordinary")
	if err := os.Mkdir(ordinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ordinary, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	removed, err := root.RemoveStaleTemps(fsroot.TempPrefix, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("RemoveStaleTemps: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stale staging directory was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "keep")); err != nil {
		t.Fatalf("a fresh staging directory was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ordinary, "keep")); err != nil {
		t.Fatalf("a non-prefixed directory was touched: %v", err)
	}
}

func TestCopyDirectoryModeExact(t *testing.T) {
	dir := t.TempDir()
	writable := filepath.Join(dir, "writable")
	readonly := filepath.Join(dir, "readonly")
	for _, src := range []string{writable, readonly} {
		if err := os.Mkdir(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(writable, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range []string{readonly, filepath.Join(dir, "readonly-dst"), filepath.Join(dir, "writable-dst")} {
			if err := os.Chmod(p, 0o755); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("restore mode on %s: %v", p, err)
			}
		}
	})
	root := openRoot(t, dir)

	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)

	if err := root.Copy(context.Background(), "writable", "writable-dst", false, nil); err != nil {
		t.Fatalf("copy a 0o770 directory: %v", err)
	}
	if err := root.Copy(context.Background(), "readonly", "readonly-dst", false, nil); err != nil {
		t.Fatalf("copy a 0o555 directory: %v", err)
	}
	modeOf := func(name string) os.FileMode {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	if got := modeOf("writable-dst"); got != 0o770 {
		t.Fatalf("copied 0o770 directory mode = %#o, want 0o770", got)
	}
	if got := modeOf("readonly-dst"); got != 0o555 {
		t.Fatalf("copied 0o555 directory mode = %#o, want 0o555", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "readonly-dst", "f")); err != nil {
		t.Fatalf("the read-only copy lost its child: %v", err)
	}
}

func modeOf(t *testing.T, dir, name string) os.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("stat %s: %v", name, err)
	}
	return info.Mode().Perm()
}

func TestChmodEntryUpdatesMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	entry, err := root.ChmodEntry("f.txt", 0o600, true)
	if err != nil {
		t.Fatalf("ChmodEntry file: %v", err)
	}
	if got := modeOf(t, dir, "f.txt"); got != 0o600 {
		t.Fatalf("file mode = %#o, want 0o600", got)
	}
	if entry.Kind != fsroot.KindRegular || entry.Mode.Perm() != 0o600 {
		t.Fatalf("entry = %+v, want regular 0o600", entry)
	}

	if _, err := root.ChmodEntry("d", 0o700, true); err != nil {
		t.Fatalf("ChmodEntry dir: %v", err)
	}
	if got := modeOf(t, dir, "d"); got != 0o700 {
		t.Fatalf("dir mode = %#o, want 0o700", got)
	}
}

func TestChmodEntryRefusesSymlinkWithoutFollowing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if _, err := root.ChmodEntry("link.txt", 0o600, true); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("ChmodEntry(symlink) = %v, want ErrInvalid", err)
	}
	if got := modeOf(t, dir, "real.txt"); got != 0o644 {
		t.Fatalf("the symlink target's mode changed to %#o, want 0o644 unchanged", got)
	}
}

func TestChmodEntryRefusesSpecialFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	root := openRoot(t, dir)

	if _, err := root.ChmodEntry("pipe", 0o644, true); !errors.Is(err, fsroot.ErrSpecial) {
		t.Fatalf("ChmodEntry(FIFO) = %v, want ErrSpecial", err)
	}
}

func TestChmodEntryRefusesRoot(t *testing.T) {
	t.Parallel()
	root := openRoot(t, t.TempDir())
	if _, err := root.ChmodEntry(".", 0o700, true); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("ChmodEntry(.) = %v, want ErrInvalid", err)
	}
}

func TestChmodEntryReadOnlyRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, err := fsroot.Open("ro", dir, info, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	if _, err := root.ChmodEntry("f.txt", 0o600, true); !errors.Is(err, fsroot.ErrReadOnly) {
		t.Fatalf("ChmodEntry on a read-only root = %v, want ErrReadOnly", err)
	}
	if got := modeOf(t, dir, "f.txt"); got != 0o644 {
		t.Fatalf("a read-only root was mutated: mode %#o", got)
	}
}

func TestChmodEntryPreservesSpecialBits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "shared"), 0o755|os.ModeSetgid|os.ModeSticky); err != nil {
		t.Skipf("cannot set special bits: %v", err)
	}
	before, err := os.Stat(filepath.Join(dir, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode()&os.ModeSetgid == 0 || before.Mode()&os.ModeSticky == 0 {
		t.Skip("the filesystem dropped the special bits")
	}
	root := openRoot(t, dir)

	if _, err := root.ChmodEntry("shared", 0o700, false); err != nil {
		t.Fatalf("ChmodEntry: %v", err)
	}
	after, err := os.Stat(filepath.Join(dir, "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0o700 {
		t.Fatalf("perm = %#o, want 0o700", after.Mode().Perm())
	}
	if after.Mode()&os.ModeSetgid == 0 || after.Mode()&os.ModeSticky == 0 {
		t.Fatalf("special bits were dropped: %v", after.Mode())
	}
}

func TestChmodEntryRefusesHardlinkInConfinedRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "f.txt"), filepath.Join(dir, "second.txt")); err != nil {
		t.Skipf("hard link: %v", err)
	}
	root := openRoot(t, dir)

	if _, err := root.ChmodEntry("f.txt", 0o600, true); !errors.Is(err, fsroot.ErrInvalid) {
		t.Fatalf("ChmodEntry(hard-linked, confined) = %v, want ErrInvalid", err)
	}
	if got := modeOf(t, dir, "f.txt"); got != 0o644 {
		t.Fatalf("a refused hard-linked chmod changed the mode: %#o", got)
	}
	if _, err := root.ChmodEntry("f.txt", 0o600, false); err != nil {
		t.Fatalf("ChmodEntry(hard-linked, whole-filesystem) = %v, want success", err)
	}
	if got := modeOf(t, dir, "f.txt"); got != 0o600 {
		t.Fatalf("mode = %#o, want 0o600", got)
	}
}

func TestChmodEntryRefusesDirectoryWithoutOwnerExecute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	for _, mode := range []os.FileMode{0o000, 0o600, 0o666} {
		if _, err := root.ChmodEntry("d", mode, false); !errors.Is(err, fsroot.ErrInvalid) {
			t.Fatalf("ChmodEntry(dir, %#o) = %v, want ErrInvalid", mode, err)
		}
	}
	if got := modeOf(t, dir, "d"); got != 0o755 {
		t.Fatalf("a refused directory chmod changed the mode: %#o", got)
	}
	if _, err := root.ChmodEntry("d", 0o700, false); err != nil {
		t.Fatalf("ChmodEntry(dir, 0o700) = %v, want success", err)
	}
}
