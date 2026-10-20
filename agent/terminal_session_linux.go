//go:build linux

package agent

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The host session cleanup walks /proc to find every process that belongs to the
// shell's session, because an interactive shell with job control puts each job
// in its own process group: signalling the shell's process group alone leaves
// `sleep N &` (and its own group) running.
const (
	// terminalScanMaxProcesses bounds one /proc pass. A fork storm must not turn
	// the close path into an unbounded walk. It is a variable so tests can inject
	// a smaller cap and exercise the truncation path.
	terminalScanMaxProcesses = 65536
)

// terminalScanLimit is terminalScanMaxProcesses; tests replace it to force a
// truncated walk.
var terminalScanLimit = terminalScanMaxProcesses

// terminalStat is the subset of /proc/<pid>/stat the session walk needs.
type terminalStat struct {
	pid       int
	state     byte
	session   int
	startTime uint64
}

// terminalSessionMember is one process found in the shell's session.
//
// fd is a pidfd opened right after the /proc record was verified, or -1 when the
// kernel (or a restriction such as seccomp) does not provide pidfds. Signalling
// through the pidfd keeps the kill bound to the process that was scanned instead
// of to a pid that may have been reused between the scan and the signal.
type terminalSessionMember struct {
	pid int
	fd  int
}

// closeMember releases the member's pidfd, if any.
func (m *terminalSessionMember) closeMember() {
	if m.fd >= 0 {
		_ = unix.Close(m.fd)
		m.fd = -1
	}
}

// closeTerminalSessionMembers releases every pidfd held by a scan.
func closeTerminalSessionMembers(members []terminalSessionMember) {
	for index := range members {
		members[index].closeMember()
	}
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

// terminalSessionMembers returns the processes in the session led by sid,
// excluding pid 1 and the agent itself. complete is false when the walk cannot
// be trusted: /proc cannot be read, the session id may have been reused by an
// unrelated session leader, or the walk hit its process cap / deadline. The
// members found so far are returned even when the walk is incomplete, so the
// caller can still signal them; callers must call closeTerminalSessionMembers for
// any slice they do not pass to signalTerminalSession.
//
// A session id is the pid of its leader, so a process whose pid equals sid is
// that leader. Its recorded start time is compared with the one captured when
// the shell started: a mismatch means the pid now belongs to a different
// process and the whole session id must not be signalled. While members of the
// original session exist the kernel keeps the id allocated, so a scan that
// finds members and no foreign leader can only see the recorded session.
//
// The walk is a single pass, capped in process count and wall-clock time. It
// never loops and never claims completeness when it was truncated.
func terminalSessionMembers(sid int, leaderStart uint64, deadline time.Time) ([]terminalSessionMember, bool) {
	if sid <= 1 || leaderStart == 0 {
		// Without a start time a reused pid cannot be told apart from the shell.
		return nil, false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	self := os.Getpid()
	members := make([]terminalSessionMember, 0, 8)
	visited := 0
	for index, entry := range entries {
		if index%64 == 0 && time.Now().After(deadline) {
			// A deadline cut is a truncated walk, never a complete empty one.
			return members, false
		}
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		if visited >= terminalScanLimit {
			// Truncated: the rest of /proc was not inspected.
			return members, false
		}
		visited++
		stat, ok := readTerminalStat(pid)
		if !ok {
			continue
		}
		if pid == sid {
			if stat.startTime != leaderStart {
				// The recorded pid now names a different session leader.
				closeTerminalSessionMembers(members)
				return nil, false
			}
		}
		if stat.state == 'Z' {
			// A zombie holds no resources and cannot be signalled; ignoring it
			// lets the walk report the session as empty.
			continue
		}
		if stat.session != sid {
			continue
		}
		if member, ok := newTerminalSessionMember(pid, stat); ok {
			members = append(members, member)
		}
	}
	return members, true
}

// newTerminalSessionMember opens a pidfd for a verified session member and
// re-reads its /proc record through the same pid to confirm that the process is
// still the one the scan saw. When pidfds are unavailable the member is kept
// with fd -1 and signalled by pid, which is the documented fallback.
func newTerminalSessionMember(pid int, stat terminalStat) (terminalSessionMember, bool) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return terminalSessionMember{}, false // exited between stat and open
		}
		// ENOSYS (old kernel), EINVAL/EPERM (seccomp/loader): fall back to kill(2).
		return terminalSessionMember{pid: pid, fd: -1}, true
	}
	verified, ok := readTerminalStat(pid)
	if !ok || verified.startTime != stat.startTime || verified.session != stat.session {
		// The pid now names a different process; never signal it.
		_ = unix.Close(fd)
		return terminalSessionMember{}, false
	}
	return terminalSessionMember{pid: pid, fd: fd}, true
}

// signalTerminalSession signals each member and releases its pidfd. The pidfd is
// used when available so the signal targets the scanned process rather than a
// pid that may have been reused; only a kernel that reports ENOSYS falls back to
// kill(2). Any other pidfd error (typically the process already exited) is
// treated as done and never retried by pid.
func signalTerminalSession(members []terminalSessionMember, signal os.Signal) {
	unixSignal, ok := signal.(syscall.Signal)
	if !ok {
		closeTerminalSessionMembers(members)
		return
	}
	self := os.Getpid()
	for index := range members {
		member := &members[index]
		pid := member.pid
		if pid <= 1 || pid == self {
			member.closeMember()
			continue
		}
		if member.fd >= 0 {
			err := unix.PidfdSendSignal(member.fd, unix.Signal(unixSignal), nil, 0)
			member.closeMember()
			if err == nil || !errors.Is(err, unix.ENOSYS) {
				continue
			}
		}
		// No pidfd (or the kernel lacks pidfd_send_signal): signal by number.
		_ = syscall.Kill(pid, unixSignal)
	}
}
