package files

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// SpoolDirEnv overrides the control-plane staging directory for streamed
// uploads (FILES_SPOOL_DIR). An empty value keeps a private 0700 directory
// under os.TempDir().
const SpoolDirEnv = "FILES_SPOOL_DIR"

// SpoolConcurrencyEnv bounds how many upload bodies may be staged to disk at
// once (FILES_SPOOL_CONCURRENCY). An unset or invalid value keeps
// DefaultSpoolConcurrency.
const SpoolConcurrencyEnv = "FILES_SPOOL_CONCURRENCY"

const (
	// DefaultSpoolConcurrency is the number of upload bodies that may be staged
	// to the spool volume at once. It is deliberately small: each staged body
	// can be as large as the upload ceiling, so a burst must not be able to
	// fill the spool filesystem.
	DefaultSpoolConcurrency = 4
	// spoolDirName is the private staging directory created under os.TempDir()
	// when FILES_SPOOL_DIR is unset.
	spoolDirName = "gotham-spool"
	// spoolFilePrefix names every staging file, so a sweep only ever removes
	// the spooler's own files.
	spoolFilePrefix = "gotham-upload-"
	// spoolFileTTL is the age after which an abandoned staging file (a crashed
	// process, a killed request) is removed by a sweep.
	spoolFileTTL = 24 * time.Hour
	// spoolSweepInterval throttles the opportunistic sweep run on the staging
	// path, so a busy control plane sweeps at most this often.
	spoolSweepInterval = time.Hour
)

// spoolFreeBytes reports the free bytes on dir's filesystem, or 0 when it
// cannot be read. It is a variable so a test can simulate a full spool volume.
var spoolFreeBytes = func(dir string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

// spooler stages streamed request bodies to disk. It bounds how many bodies are
// staged concurrently and keeps its staging files in a private directory,
// sweeping abandoned files so a crash cannot leak disk space.
type spooler struct {
	dir    string
	sem    chan struct{}
	logger *slog.Logger
	mu     sync.Mutex
	swept  time.Time
}

// newSpooler builds a spooler. dir and concurrency fall back to FILES_SPOOL_DIR
// and FILES_SPOOL_CONCURRENCY, then to a private directory under os.TempDir()
// and DefaultSpoolConcurrency. It creates the directory 0700 and sweeps stale
// files once at construction (the startup sweep).
func newSpooler(dir string, concurrency int, logger *slog.Logger) *spooler {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(dir) == "" {
		dir = strings.TrimSpace(os.Getenv(SpoolDirEnv))
	}
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join(os.TempDir(), spoolDirName)
	}
	if concurrency <= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(SpoolConcurrencyEnv))); err == nil && n > 0 {
			concurrency = n
		}
	}
	if concurrency <= 0 {
		concurrency = DefaultSpoolConcurrency
	}
	s := &spooler{dir: dir, sem: make(chan struct{}, concurrency), logger: logger}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Warn("files: cannot create the upload spool directory", "dir", dir, "error", err)
	}
	s.sweep(time.Now())
	return s
}

// stage streams body into a temp file, returning the rewound file, its exact
// size and a cleanup the caller must run. cleanup removes the file and releases
// the staging slot, so it must run on every path (success, error, cancel). It
// refuses with ErrTooLarge once limit bytes are exceeded, with
// ErrInsufficientStorage when the spool filesystem cannot hold limit bytes, and
// with ErrBusy when too many bodies are already staged.
func (s *spooler) stage(body io.Reader, limit int64) (*os.File, int64, func(), error) {
	select {
	case s.sem <- struct{}{}:
	default:
		return nil, 0, func() {}, fmt.Errorf("%w: too many uploads are being staged", ErrBusy)
	}
	release := func() { <-s.sem }

	// Refuse before writing anything when the spool volume cannot hold a
	// ceiling-sized body. A statfs that cannot be read (free == 0) is not
	// treated as full: the agent re-checks the target's own filesystem at
	// commit, which is the authoritative guard.
	if free := spoolFreeBytes(s.dir); free > 0 && uint64(limit) > free {
		release()
		return nil, 0, func() {}, fmt.Errorf("%w: %d bytes of spool space are not available", ErrInsufficientStorage, limit)
	}
	s.maybeSweep(time.Now())

	file, err := os.CreateTemp(s.dir, spoolFilePrefix+"*")
	if err != nil {
		release()
		return nil, 0, func() {}, fmt.Errorf("%w: staging the upload", ErrValidation)
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
		release()
	}
	written, err := io.Copy(file, io.LimitReader(body, limit+1))
	if err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("%w: reading the upload: %w", ErrValidation, err)
	}
	if written > limit {
		cleanup()
		return nil, 0, func() {}, ErrTooLarge
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("%w: staging the upload", ErrValidation)
	}
	return file, written, cleanup, nil
}

// maybeSweep runs the periodic stale-file sweep at most once per
// spoolSweepInterval. It is driven by the staging path rather than a timer
// goroutine, so the spooler needs no lifecycle to stop: a control plane that
// never stages an upload also has nothing to sweep. The startup sweep in
// newSpooler covers the rest.
// ponytail: activity-driven sweep, not a ticker; add a server-lifecycle ticker
// if stale files must be removed while the control plane is idle.
func (s *spooler) maybeSweep(now time.Time) {
	s.mu.Lock()
	due := now.Sub(s.swept) >= spoolSweepInterval
	if due {
		s.swept = now
	}
	s.mu.Unlock()
	if due {
		s.sweep(now)
	}
}

// sweep removes staging files older than spoolFileTTL. It only ever touches
// regular files carrying the spool prefix, so it cannot remove an unrelated
// file that happens to share the directory.
func (s *spooler) sweep(now time.Time) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-spoolFileTTL)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), spoolFilePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(s.dir, entry.Name()))
	}
}
