//go:build !linux

package terminal

import (
	"os"
	"time"
)

func terminalProcessStartTime(pid int) uint64 { return 0 }

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
