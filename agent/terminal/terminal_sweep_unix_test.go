//go:build unix

package terminal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepStaleTerminalRcDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	now := time.Now()
	old := now.Add(-25 * time.Hour)

	makeDir := func(name string, modTime time.Time) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
		return path
	}

	stale := makeDir(terminalRcDirPrefix+"stale", old)
	fresh := makeDir(terminalRcDirPrefix+"fresh", now)
	otherPrefix := makeDir("x-"+terminalRcDirPrefix+"stale", old)
	unrelated := makeDir("unrelated-stale", old)

	oldFile := filepath.Join(root, terminalRcDirPrefix+"file")
	if err := os.WriteFile(oldFile, []byte("not a session dir"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chtimes(oldFile, old, old); err != nil {
		t.Fatalf("chtimes file: %v", err)
	}

	link := filepath.Join(root, terminalRcDirPrefix+"link")
	if err := os.Symlink(unrelated, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	sweepStaleTerminalRcDirs(now)

	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale integration directory still exists: %v", err)
	}
	for _, path := range []string{fresh, otherPrefix, unrelated, oldFile, link} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("sweep removed %s: %v", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("the symlink target was followed and removed: %v", err)
	}
}

func TestSweepStaleTerminalRcDirsIgnoresUnreadableTempDir(t *testing.T) {
	sweepStaleTerminalRcDirsIn(filepath.Join(t.TempDir(), "missing"), time.Now())
}
