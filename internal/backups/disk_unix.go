//go:build unix

package backups

import "syscall"

func diskStats(dir string) (free, total int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, 0, err
	}
	block := int64(stat.Bsize)
	return int64(stat.Bavail) * block, int64(stat.Blocks) * block, nil
}
