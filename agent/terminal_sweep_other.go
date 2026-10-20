//go:build !unix

package agent

import "time"

// sweepStaleTerminalRcDirs is a no-op on non-unix hosts: the agent is not
// supported there and the unix ownership check does not apply.
func sweepStaleTerminalRcDirs(time.Time) {}
