//go:build !unix

package backups

import "errors"

func diskStats(string) (free, total int64, err error) {
	return 0, 0, errors.New("backups: free space is not available on this platform")
}
