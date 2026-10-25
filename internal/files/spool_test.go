package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpoolSweepsStaleFiles(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, spoolFilePrefix+"stale")
	fresh := filepath.Join(dir, spoolFilePrefix+"fresh")
	unrelated := filepath.Join(dir, "keep-me")
	for _, name := range []string{stale, fresh, unrelated} {
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	old := time.Now().Add(-2 * spoolFileTTL)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("age stale file: %v", err)
	}

	newSpooler(dir, 1, nil)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale staging file was not swept: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh staging file was swept: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("an unrelated file was swept: %v", err)
	}
}

func TestSpoolBoundsConcurrency(t *testing.T) {
	s := newSpooler(t.TempDir(), 1, nil)

	_, _, cleanup, err := s.stage(strings.NewReader("abc"), 100)
	if err != nil {
		t.Fatalf("first stage: %v", err)
	}
	if _, _, _, err := s.stage(strings.NewReader("de"), 100); !errors.Is(err, ErrSpoolBusy) {
		t.Fatalf("second stage err = %v, want ErrSpoolBusy", err)
	}

	cleanup()
	_, _, cleanup2, err := s.stage(strings.NewReader("de"), 100)
	if err != nil {
		t.Fatalf("stage after release: %v", err)
	}
	cleanup2()
}

func TestSpoolCleanupIsIdempotent(t *testing.T) {
	s := newSpooler(t.TempDir(), 2, nil)
	_, _, cleanup, err := s.stage(strings.NewReader("x"), 100)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	cleanup()
	cleanup()

	if got := len(s.sem); got != 0 {
		t.Fatalf("semaphore slots held after a double cleanup = %d, want 0", got)
	}
	_, _, cleanup2, err := s.stage(strings.NewReader("y"), 100)
	if err != nil {
		t.Fatalf("stage after a double cleanup: %v", err)
	}
	cleanup2()
}

func TestSpoolRefusesWhenVolumeFull(t *testing.T) {
	original := spoolFreeBytes
	spoolFreeBytes = func(string) uint64 { return 1 }
	t.Cleanup(func() { spoolFreeBytes = original })

	s := newSpooler(t.TempDir(), 1, nil)
	if _, _, _, err := s.stage(strings.NewReader("x"), 100); !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("stage err = %v, want ErrInsufficientStorage", err)
	}
}

func TestSpoolUsesPrivateDirAndFileModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	s := newSpooler(dir, 1, nil)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat spool dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("spool dir mode = %o, want 0700", perm)
	}

	file, _, cleanup, err := s.stage(strings.NewReader("x"), 100)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	defer cleanup()
	fileInfo, err := os.Stat(file.Name())
	if err != nil {
		t.Fatalf("stat staging file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("staging file mode = %o, want 0600", perm)
	}
	if base := filepath.Base(file.Name()); !strings.HasPrefix(base, spoolFilePrefix) {
		t.Fatalf("staging file %q does not carry the spool prefix", base)
	}
}
