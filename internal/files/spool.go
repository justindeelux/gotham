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

const SpoolDirEnv = "FILES_SPOOL_DIR"

const SpoolConcurrencyEnv = "FILES_SPOOL_CONCURRENCY"

const (
	DefaultSpoolConcurrency = 4
	spoolDirName            = "gotham-spool"
	spoolFilePrefix         = "gotham-upload-"
	spoolFileTTL            = 24 * time.Hour
	spoolSweepInterval      = time.Hour
)

var spoolFreeBytes = func(dir string) uint64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize)
}

type spooler struct {
	dir    string
	sem    chan struct{}
	logger *slog.Logger
	mu     sync.Mutex
	swept  time.Time
}

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
	s := &spooler{
		dir:    dir,
		sem:    make(chan struct{}, concurrency),
		logger: logger,
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Warn("files: cannot create the upload spool directory", "dir", dir, "error", err)
	}
	s.sweep(time.Now())
	return s
}

func (s *spooler) stage(body io.Reader, limit int64) (*os.File, int64, func(), error) {
	select {
	case s.sem <- struct{}{}:
	default:
		return nil, 0, func() {}, ErrSpoolBusy
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			<-s.sem
		})
	}

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
