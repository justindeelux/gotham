//go:build !unix

package storages

import "errors"

func DiskStats(string) (free, total int64, err error) {
	return 0, 0, errors.New("storages: free space is not available on this platform")
}
