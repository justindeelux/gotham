//go:build !linux

package files

import "os"

func preallocate(*os.File, int64) error { return nil }
