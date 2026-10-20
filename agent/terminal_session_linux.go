//go:build linux

package agent

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The host session cleanup walks /proc to find every process that belongs to the
// shell's session, because an interactive shell with job control puts each job
// in its own process group: signalling the shell's process group alone leaves
// `sleep N &` (and its own group) running.
const (
	// terminalScanMaxProcesses bounds one /proc pass. A fork storm must not turn
	// the close path into an unbounded walk.
	terminalScanMaxProcesses = 8192
)

// terminalStat is the subset of /proc/<pid>/stat the session walk needs.
type terminalStat struct {
	pid       int
	state     byte
	session   int
	startTime uint64
}

// terminalProcessStartTime reports a process's start time in clock ticks since
// boot (field 22 of /proc/<pid>/stat), or 0 when the process is gone or the
// record cannot be read. The value guards against a reused pid.
func terminalProcessStartTime(pid int) uint64 {
	stat, ok := readTerminalStat(pid)
	if !ok || stat.pid != pid {
		return 0
	}
	return stat.startTime
}

// readTerminalStat parses /proc/<pid>/stat.
func readTerminalStat(pid int) (terminalStat, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return terminalStat{}, false
	}
	return parseTerminalStat(pid, data)
}

// parseTerminalStat parses one /proc/<pid>/stat record. The comm field is
// enclosed in parentheses and may itself contain spaces, parentheses and
// newlines, so everything is parsed from the LAST ')' onwards. Field 3 (state)
// is the first token after it; session is field 6 and start time is field 22.
func parseTerminalStat(pid int, data []byte) (terminalStat, bool) {
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return terminalStat{}, false
	}
	fields := strings.Fields(string(data[end+1:]))
	// state, ppid, pgrp, session ... starttime is the 20th token after ")".
	if len(fields) < 20 || len(fields[0]) == 0 {
		return terminalStat{}, false
	}
	session, err := strconv.Atoi(fields[3])
	if err != nil || session <= 0 {
		return terminalStat{}, false
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return terminalStat{}, false
	}
	return terminalStat{pid: pid, state: fields[0][0], session: session, startTime: startTime}, true
}

// terminalSessionMembers returns the pids in the session led by sid, excluding
// pid 1 and the agent itself. ok is false when the walk cannot be trusted: the
// session id may have been reused by an unrelated session leader, or /proc
// cannot be read. An empty, true result means the session has no processes left.
//
// A session id is the pid of its leader, so a process whose pid equals sid is
// that leader. Its recorded start time is compared with the one captured when
// the shell started: a mismatch means the pid now belongs to a different
// process and the whole session id must not be signalled. While members of the
// original session exist the kernel keeps the id allocated, so a scan that
// finds members and no foreign leader can only see the recorded session.
//
// The walk is a single pass, capped in process count and wall-clock time. A
// partial walk still returns the members found so far; it never loops.
func terminalSessionMembers(sid int, leaderStart uint64, deadline time.Time) ([]int, bool) {
	if sid <= 1 || leaderStart == 0 {
		// Without a start time a reused pid cannot be told apart from the shell.
		return nil, false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	self := os.Getpid()
	members := make([]int, 0, 8)
	visited := 0
	for index, entry := range entries {
		if index%64 == 0 && time.Now().After(deadline) {
			return members, true
		}
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		if visited >= terminalScanMaxProcesses {
			return members, true
		}
		visited++
		stat, ok := readTerminalStat(pid)
		if !ok {
			continue
		}
		if pid == sid {
			if stat.startTime != leaderStart {
				// The recorded pid now names a different session leader.
				return nil, false
			}
		}
		if stat.state == 'Z' {
			// A zombie holds no resources and cannot be signalled; ignoring it
			// lets the walk report the session as empty.
			continue
		}
		if stat.session == sid {
			members = append(members, pid)
		}
	}
	return members, true
}

// signalTerminalSession signals each pid directly. Only processes matched by
// terminalSessionMembers reach this function, so pid 1 and unrelated processes
// are never addressed. A process that changed uid (su/sudo) is still matched by
// its session id; a root agent can signal it, a non-root agent's kill fails
// with EPERM and is ignored (documented residual limit).
func signalTerminalSession(pids []int, signal os.Signal) {
	unixSignal, ok := signal.(syscall.Signal)
	if !ok {
		return
	}
	self := os.Getpid()
	for _, pid := range pids {
		if pid <= 1 || pid == self {
			continue
		}
		_ = syscall.Kill(pid, unixSignal)
	}
}
