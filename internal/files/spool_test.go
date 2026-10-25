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

	newSpooler(dir, 1, 0, nil)

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
	s := newSpooler(t.TempDir(), 1, 0, nil)

	_, _, cleanup, err := s.stage("", strings.NewReader("abc"), 100)
	if err != nil {
		t.Fatalf("first stage: %v", err)
	}
	if _, _, _, err := s.stage("", strings.NewReader("de"), 100); !errors.Is(err, ErrBusy) {
		t.Fatalf("second stage err = %v, want ErrBusy", err)
	}

	cleanup()
	_, _, cleanup2, err := s.stage("", strings.NewReader("de"), 100)
	if err != nil {
		t.Fatalf("stage after release: %v", err)
	}
	cleanup2()
}

func TestSpoolCapsEachTeam(t *testing.T) {
	s := newSpooler(t.TempDir(), 3, 1, nil)

	_, _, cleanupA, err := s.stage("team:a", strings.NewReader("abc"), 100)
	if err != nil {
		t.Fatalf("first team stage: %v", err)
	}
	if _, _, _, err := s.stage("team:a", strings.NewReader("de"), 100); !errors.Is(err, ErrTeamBusy) {
		t.Fatalf("second team stage err = %v, want ErrTeamBusy", err)
	}
	_, _, cleanupB, err := s.stage("team:b", strings.NewReader("fg"), 100)
	if err != nil {
		t.Fatalf("a second team could not stage while the first held its share: %v", err)
	}

	cleanupA()
	_, _, cleanupA2, err := s.stage("team:a", strings.NewReader("hi"), 100)
	if err != nil {
		t.Fatalf("stage after the team share was released: %v", err)
	}
	cleanupA2()
	cleanupB()
}

func TestSpoolTeamShareDefaultsAndSetting(t *testing.T) {
	t.Setenv(SpoolTeamConcurrencyEnv, "")
	for _, tc := range []struct{ concurrency, want int }{{1, 1}, {2, 1}, {3, 1}, {4, 2}, {8, 4}} {
		s := newSpooler(t.TempDir(), tc.concurrency, 0, nil)
		if s.teamCap != tc.want {
			t.Fatalf("concurrency %d: team share = %d, want %d", tc.concurrency, s.teamCap, tc.want)
		}
	}

	t.Setenv(SpoolTeamConcurrencyEnv, "3")
	if s := newSpooler(t.TempDir(), 4, 0, nil); s.teamCap != 3 {
		t.Fatalf("configured team share = %d, want 3", s.teamCap)
	}
	if s := newSpooler(t.TempDir(), 4, 9, nil); s.teamCap != 4 {
		t.Fatalf("team share above the global limit = %d, want 4", s.teamCap)
	}
}

func TestSpoolTeamShareReleasedByDeferredCleanup(t *testing.T) {
	s := newSpooler(t.TempDir(), 1, 1, nil)
	func() {
		defer func() { _ = recover() }()
		_, _, cleanup, err := s.stage("team:a", strings.NewReader("x"), 100)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		defer cleanup()
		panic("simulated handler panic")
	}()

	_, _, cleanup, err := s.stage("team:a", strings.NewReader("y"), 100)
	if err != nil {
		t.Fatalf("stage after a panic: %v", err)
	}
	cleanup()
}

func TestSpoolRefusesWhenVolumeFull(t *testing.T) {
	original := spoolFreeBytes
	spoolFreeBytes = func(string) uint64 { return 1 }
	t.Cleanup(func() { spoolFreeBytes = original })

	s := newSpooler(t.TempDir(), 1, 0, nil)
	if _, _, _, err := s.stage("", strings.NewReader("x"), 100); !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("stage err = %v, want ErrInsufficientStorage", err)
	}
}

func TestSpoolUsesPrivateDirAndFileModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	s := newSpooler(dir, 1, 0, nil)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat spool dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("spool dir mode = %o, want 0700", perm)
	}

	file, _, cleanup, err := s.stage("", strings.NewReader("x"), 100)
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
