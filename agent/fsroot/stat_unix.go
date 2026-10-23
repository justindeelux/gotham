//go:build unix

package fsroot

import (
	"fmt"
	"os"
	"syscall"
)

func devino(info os.FileInfo) (dev, ino uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino)
}

func etag(info os.FileInfo) string {
	dev, ino := devino(info)
	return fmt.Sprintf("%q", fmt.Sprintf("%d:%d:%d:%d", dev, ino, info.ModTime().UnixNano(), info.Size()))
}

func ownerLinks(info os.FileInfo) (uid, gid uint32, nlink uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0
	}
	return st.Uid, st.Gid, uint64(st.Nlink)
}
