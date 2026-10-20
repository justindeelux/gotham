//go:build !linux

package agent

import (
	"os"
	"time"
)

// Non-Linux hosts have no /proc session walk. The host session keeps the
// process-group cleanup: background jobs in their own process groups can
// survive a close there. The supported agent targets are Linux.

func terminalProcessStartTime(pid int) uint64 { return 0 }

func terminalSessionMembers(sid int, leaderStart uint64, deadline time.Time) ([]int, bool) {
	return nil, false
}

func signalTerminalSession(pids []int, signal os.Signal) {}
