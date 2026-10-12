//go:build !linux

package agent

import "os"

// preallocate is a no-op on platforms without fallocate (the agent's dev
// platform, macOS): the in-place write keeps its free-space check as the guard.
func preallocate(*os.File, int64) error { return nil }
