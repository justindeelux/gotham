//go:build unix

package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const terminalRcDirMaxAge = 24 * time.Hour

func sweepStaleTerminalRcDirs(now time.Time) {
	sweepStaleTerminalRcDirsIn(os.TempDir(), now)
}

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
