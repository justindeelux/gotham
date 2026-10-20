//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// terminalRcDirMaxAge is how old an orphaned integration directory must be
// before the startup sweep removes it. A live session's directory is minutes
// old at most, so a day leaves room for a slow start and clock skew.
const terminalRcDirMaxAge = 24 * time.Hour

// sweepStaleTerminalRcDirs removes integration directories orphaned by a hard
// kill (SIGKILL, OOM, panic) of a previous agent process. The normal path
// removes the directory in terminalSession.wait, so this only cleans leftovers.
// It is deliberately conservative: only our prefix, only directories (never a
// symlink, which could redirect the removal), only entries owned by this uid,
// only older than terminalRcDirMaxAge, and every error is ignored so startup
// is never blocked by a full or unreadable temp directory.
func sweepStaleTerminalRcDirs(now time.Time) {
	sweepStaleTerminalRcDirsIn(os.TempDir(), now)
}

// sweepStaleTerminalRcDirsIn is sweepStaleTerminalRcDirs with an explicit
// directory and clock for tests.
func sweepStaleTerminalRcDirsIn(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	uid := uint32(os.Getuid())
	cutoff := now.Add(-terminalRcDirMaxAge)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, terminalRcDirPrefix) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		// Lstat: a symlink is reported as a symlink and never followed.
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uid {
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}
