//go:build linux

package terminal

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	terminalScanMaxProcesses = 65536
	terminalConcurrentScans  = 2
)

var (
	terminalScanLimit  = terminalScanMaxProcesses
	terminalScanSlots  = make(chan struct{}, terminalConcurrentScans)
	terminalScanStat   = readTerminalStatErr
	terminalVerifyStat = readTerminalStatErr
)

var errTerminalStatUnparseable = errors.New("terminal: unparseable /proc stat")

type terminalStat struct {
	pid       int
	state     byte
	session   int
	startTime uint64
}

type terminalSessionMember struct {
	pid       int
	fd        int
	session   int
	startTime uint64
}

func (m *terminalSessionMember) closeMember() {
	if m.fd >= 0 {
		_ = unix.Close(m.fd)
		m.fd = -1
	}
}

func closeTerminalSessionMembers(members []terminalSessionMember) {
	for index := range members {
		members[index].closeMember()
	}
}

func mergeTerminalSessionMembers(kept, found []terminalSessionMember) []terminalSessionMember {
	if len(found) == 0 {
		return kept
	}
	known := make(map[int]bool, len(kept))
	for index := range kept {
		known[kept[index].pid] = true
	}
	for index := range found {
		member := found[index]
		if known[member.pid] {
			member.closeMember()
			continue
		}
		known[member.pid] = true
		kept = append(kept, member)
	}
	return kept
}

func anyTerminalSessionMemberAlive(members []terminalSessionMember) bool {
	for index := range members {
		member := &members[index]
		if member.fd < 0 {
			return true
		}
		if err := unix.PidfdSendSignal(member.fd, 0, nil, 0); err != nil && !errors.Is(err, unix.EPERM) {
			continue
		}
		if stat, ok := readTerminalStat(member.pid); ok && stat.state == 'Z' {
			continue
		}
		return true
	}
	return false
}

func terminalProcessStartTime(pid int) uint64 {
	stat, ok := readTerminalStat(pid)
	if !ok || stat.pid != pid {
		return 0
	}
	return stat.startTime
}

func readTerminalStat(pid int) (terminalStat, bool) {
	stat, err := readTerminalStatErr(pid)
	if err != nil {
		return terminalStat{}, false
	}
	return stat, true
}

func readTerminalStatErr(pid int) (terminalStat, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return terminalStat{}, err
	}
	stat, ok := parseTerminalStat(pid, data)
	if !ok {
		return terminalStat{}, errTerminalStatUnparseable
	}
	return stat, nil
}

func parseTerminalStat(pid int, data []byte) (terminalStat, bool) {
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return terminalStat{}, false
	}
	fields := strings.Fields(string(data[end+1:]))
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

func terminalSessionMembers(sid int, leaderStart uint64, deadline time.Time) ([]terminalSessionMember, bool) {
	if sid <= 1 || leaderStart == 0 {
		return nil, false
	}
	if !acquireTerminalScanSlot(deadline) {
		return nil, false
	}
	defer releaseTerminalScanSlot()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	self := os.Getpid()
	members := make([]terminalSessionMember, 0, 8)
	unreadable := 0
	visited := 0
	for index, entry := range entries {
		if index%64 == 0 && time.Now().After(deadline) {
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
			return members, false
		}
		visited++
		stat, err := terminalScanStat(pid)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			unreadable++
			continue
		}
		if pid == sid {
			if stat.startTime != leaderStart {
				closeTerminalSessionMembers(members)
				return nil, false
			}
		}
		if stat.state == 'Z' {
			continue
		}
		if stat.session != sid {
			continue
		}
		if member, ok := newTerminalSessionMember(pid, stat); ok {
			members = append(members, member)
		}
	}
	return members, unreadable == 0
}

func acquireTerminalScanSlot(deadline time.Time) bool {
	select {
	case terminalScanSlots <- struct{}{}:
		return true
	default:
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case terminalScanSlots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

func releaseTerminalScanSlot() { <-terminalScanSlots }

func newTerminalSessionMember(pid int, stat terminalStat) (terminalSessionMember, bool) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return terminalSessionMember{}, false
		}
		return terminalSessionMember{pid: pid, fd: -1, session: stat.session, startTime: stat.startTime}, true
	}
	verified, err := terminalVerifyStat(pid)
	if err != nil || verified.startTime != stat.startTime || verified.session != stat.session {
		_ = unix.Close(fd)
		return terminalSessionMember{}, false
	}
	return terminalSessionMember{pid: pid, fd: fd, session: stat.session, startTime: stat.startTime}, true
}

func signalTerminalSession(members []terminalSessionMember, signal os.Signal) {
	unixSignal, ok := signal.(syscall.Signal)
	if !ok {
		return
	}
	self := os.Getpid()
	for index := range members {
		member := &members[index]
		pid := member.pid
		if pid <= 1 || pid == self {
			continue
		}
		if member.fd >= 0 {
			err := unix.PidfdSendSignal(member.fd, unix.Signal(unixSignal), nil, 0)
			if err == nil || !errors.Is(err, unix.ENOSYS) {
				continue
			}
		}
		verified, err := terminalVerifyStat(pid)
		if err != nil || verified.startTime != member.startTime || verified.session != member.session {
			continue
		}
		_ = syscall.Kill(pid, unixSignal)
	}
}
