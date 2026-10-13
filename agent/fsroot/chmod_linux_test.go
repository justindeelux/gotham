//go:build linux

package fsroot_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/justindeelux/gotham/agent/fsroot"
)

// requireUnreadable fails the test unless the entry is genuinely unreadable by
// this process, so a root (or otherwise privileged) run skips instead of
// asserting a recovery it cannot exercise.
func requireUnreadable(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err == nil {
		_ = f.Close()
		t.Skip("running as a privileged user; the entry is still readable")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("open %s = %v, want EACCES", path, err)
	}
}

// TestChmodEntryRecoversUnreadableFile pins the recovery contract: a file the
// agent chmod-ed to 000 (so it can no longer be opened O_RDONLY) can still be
// chmod-ed back through the O_PATH descriptor.
func TestChmodEntryRecoversUnreadableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if _, err := root.ChmodEntry("f.txt", 0o000, false); err != nil {
		t.Fatalf("chmod to 000: %v", err)
	}
	requireUnreadable(t, target)

	entry, err := root.ChmodEntry("f.txt", 0o644, false)
	if err != nil {
		t.Fatalf("recover 000 file: %v", err)
	}
	if got := modeOf(t, dir, "f.txt"); got != 0o644 {
		t.Fatalf("mode = %#o, want 0o644", got)
	}
	if entry.Kind != fsroot.KindRegular || entry.Mode.Perm() != 0o644 {
		t.Fatalf("entry = %+v, want regular 0o644", entry)
	}
}

// TestChmodEntryRecoversUnsearchableDirectory pins the same contract for a
// directory: a directory left at 000 by another actor (the product itself
// refuses to remove a directory's owner execute) must still be recoverable.
func TestChmodEntryRecoversUnsearchableDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "d")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if err := os.Chmod(target, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o755) })
	requireUnreadable(t, target)

	if _, err := root.ChmodEntry("d", 0o755, false); err != nil {
		t.Fatalf("recover 000 directory: %v", err)
	}
	if got := modeOf(t, dir, "d"); got != 0o755 {
		t.Fatalf("mode = %#o, want 0o755", got)
	}
}

// TestChmodEntryFollowsIntermediateSymlink pins that a symlinked parent is
// resolved inside the root (as os.Root did) rather than refused. The manual
// component walk used when openat2 is unavailable refuses it, so that case is
// skipped.
func TestChmodEntryFollowsIntermediateSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	if _, err := root.ChmodEntry("link/f.txt", 0o600, false); err != nil {
		if errors.Is(err, fsroot.ErrInvalid) {
			t.Skip("intermediate symlink resolution needs openat2")
		}
		t.Fatalf("chmod through a symlinked parent: %v", err)
	}
	if got := modeOf(t, filepath.Join(dir, "real"), "f.txt"); got != 0o600 {
		t.Fatalf("mode = %#o, want 0o600", got)
	}
}

// TestChmodEntryUnderUnsearchableParent pins that resolving the parent needs
// only search (execute) permission, not read: a file inside a 0o111 directory
// can still be chmod-ed and its entry returned.
func TestChmodEntryUnderUnsearchableParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parent := filepath.Join(dir, "d")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := openRoot(t, dir)

	// 0o111: traversable but not listable/readable.
	if _, err := root.ChmodEntry("d", 0o111, false); err != nil {
		t.Fatalf("chmod parent to 0o111: %v", err)
	}
	// Restore before t.TempDir's RemoveAll, which must list the directory.
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	requireUnreadable(t, parent)

	entry, err := root.ChmodEntry("d/f.txt", 0o600, false)
	if err != nil {
		t.Fatalf("chmod under a 0o111 parent: %v", err)
	}
	if got := modeOf(t, parent, "f.txt"); got != 0o600 {
		t.Fatalf("mode = %#o, want 0o600", got)
	}
	if entry.Mode.Perm() != 0o600 {
		t.Fatalf("returned mode = %#o, want 0o600", entry.Mode.Perm())
	}
}
