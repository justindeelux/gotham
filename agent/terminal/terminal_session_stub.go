//go:build !linux

package terminal

import (
	"os"
	"time"
)

// Non-Linux hosts have no /proc session walk. The host session keeps the
// process-group cleanup: background jobs in their own process groups can
// survive a close there. The supported agent targets are Linux.

func terminalProcessStartTime(pid int) uint64 { return 0 }

// terminalSessionMember mirrors the Linux type; no member is ever produced and
// the fields carry no meaning without a /proc walk.
type terminalSessionMember struct{}

func closeTerminalSessionMembers(members []terminalSessionMember) {}

func mergeTerminalSessionMembers(kept, found []terminalSessionMember) []terminalSessionMember {
	return kept
}

func anyTerminalSessionMemberAlive(members []terminalSessionMember) bool { return false }

func terminalSessionMembers(sid int, leaderStart uint64, deadline time.Time) ([]terminalSessionMember, bool) {
	return nil, false
}

func signalTerminalSession(members []terminalSessionMember, signal os.Signal) {}
