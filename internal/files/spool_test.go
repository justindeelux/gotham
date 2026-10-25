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
	if _, _, _, err := s.stage("", strings.NewReader("de"), 100); !errors.Is(err, ErrSpoolBusy) {
		t.Fatalf("second stage err = %v, want ErrSpoolBusy", err)
	}

	cleanup()
	_, _, cleanup2, err := s.stage("", strings.NewReader("de"), 100)
	if err != nil {
		t.Fatalf("stage after release: %v", err)
	}
	cleanup2()
}

func TestSpoolLoneTeamUsesGlobalCeiling(t *testing.T) {
	s := newSpooler(t.TempDir(), 4, 0, nil)
	cleanups := make([]func(), 0, 4)
	for i := range 4 {
		_, _, cleanup, err := s.stage("team:a", strings.NewReader("x"), 100)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		cleanups = append(cleanups, cleanup)
	}
	if _, _, _, err := s.stage("team:a", strings.NewReader("x"), 100); !errors.Is(err, ErrSpoolBusy) {
		t.Fatalf("stage beyond the global ceiling err = %v, want ErrSpoolBusy", err)
	}
	for _, cleanup := range cleanups {
		cleanup()
	}
}

func TestSpoolShareOnlyWhileAnotherTeamIsActive(t *testing.T) {
	s := newSpooler(t.TempDir(), 5, 2, nil)
	take := func(owner string) func() {
		t.Helper()
		_, _, cleanup, err := s.stage(owner, strings.NewReader("x"), 100)
		if err != nil {
			t.Fatalf("stage %s: %v", owner, err)
		}
		return cleanup
	}

	var heldA []func()
	for range 5 {
		heldA = append(heldA, take("team:a"))
	}
	if _, _, _, err := s.stage("team:b", strings.NewReader("x"), 100); !errors.Is(err, ErrSpoolBusy) {
		t.Fatalf("team B staged while A held every slot: %v", err)
	}

	// As A frees slots, B gets in.
	heldA[0]()
	heldA[1]()
	heldA = heldA[2:]
	heldB := []func(){take("team:b"), take("team:b")}

	// A is above its share while B is active: freeing one A slot does not let
	// A take it back, and B is already at its share.
	heldA[0]()
	heldA = heldA[1:]
	if _, _, _, err := s.stage("team:a", strings.NewReader("x"), 100); !errors.Is(err, ErrTeamBusy) {
		t.Fatalf("team A re-took a slot above its share: %v", err)
	}
	if _, _, _, err := s.stage("team:b", strings.NewReader("x"), 100); !errors.Is(err, ErrTeamBusy) {
		t.Fatalf("team B exceeded its share: %v", err)
	}

	// Below its share, A can take a slot again.
	heldA[0]()
	heldA = heldA[1:]
	heldA = append(heldA, take("team:a"))

	// With B gone, A may once again use the whole global ceiling.
	for _, cleanup := range heldB {
		cleanup()
	}
	for range 3 {
		heldA = append(heldA, take("team:a"))
	}
	if _, _, _, err := s.stage("team:a", strings.NewReader("x"), 100); !errors.Is(err, ErrSpoolBusy) {
		t.Fatalf("a lone team again exceeded the global ceiling: %v", err)
	}
	for _, cleanup := range heldA {
		cleanup()
	}
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

func TestSpoolCleanupIsIdempotent(t *testing.T) {
	s := newSpooler(t.TempDir(), 2, 1, nil)
	_, _, cleanup, err := s.stage("team:a", strings.NewReader("x"), 100)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	cleanup()
	cleanup()

	if got := len(s.sem); got != 0 {
		t.Fatalf("semaphore slots held after a double cleanup = %d, want 0", got)
	}
	s.mu.Lock()
	staged := len(s.staged)
	s.mu.Unlock()
	if staged != 0 {
		t.Fatalf("owners still staged after a double cleanup = %d, want 0", staged)
	}
	_, _, cleanup2, err := s.stage("team:a", strings.NewReader("y"), 100)
	if err != nil {
		t.Fatalf("stage after a double cleanup: %v", err)
	}
	cleanup2()
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
