//go:build !unix

package terminal

import "time"

func sweepStaleTerminalRcDirs(time.Time) {}
