//go:build unix

package fsroot

import (
	"fmt"
	"os"
	"syscall"
)

// devino returns the device and inode numbers of an entry. Both syscall.Stat_t
// and unix.Stat_t name these fields Dev and Ino on every unix platform the
// agent targets, so one conversion covers Linux and Darwin.
func devino(info os.FileInfo) (dev, ino uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino)
}

// etag is the RFC 7232 quoted entity tag "dev:ino:mtime_ns:size". It changes
// when the entry's identity, content time or length changes, so a write can
// detect that the file it read is no longer the file on disk.
func etag(info os.FileInfo) string {
	dev, ino := devino(info)
	return fmt.Sprintf("%q", fmt.Sprintf("%d:%d:%d:%d", dev, ino, info.ModTime().UnixNano(), info.Size()))
}
