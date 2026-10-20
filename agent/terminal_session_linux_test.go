//go:build linux

package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// These tests exercise the host session cleanup on a real PTY. They are
// Linux-only because the cleanup attributes processes through /proc. Every test
// is bounded: at most a handful of processes per case, hard deadlines, and a
// cleanup that kills a recorded pid (or a uniquely named escaped `sleep`) even
// when the test fails early.

// terminalPidFile returns a pid file path under the fixture root.
func terminalPidFile(root, name string) string { return filepath.Join(root, name) }

// sendTerminalInput writes one command line to the shell.
func sendTerminalInput(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient, line string) {
	t.Helper()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte(line)}}); err != nil {
		t.Fatalf("send input %q: %v", line, err)
	}
}

// closeTerminalSession sends a confirmed close and waits for the stream to end.
func closeTerminalSession(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient) {
	t.Helper()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Close{
		Close: &agentv1.TerminalClose{Reason: "test_close"},
	}}); err != nil {
		t.Fatalf("send close: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, err := stream.Recv(); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("terminal stream did not end after close")
	}
}

// waitForProcessGone fails when pid is still alive at the deadline.
func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	const timeout = 8 * time.Second
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		// A zombie is not running. Orphaned jobs can stay zombies forever when
		// the test binary runs as pid 1 (container proofs), because pid 1 here
		// does not reap them.
		if stat, ok := readTerminalStat(pid); ok && stat.state == 'Z' {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after %s", pid, timeout)
}

// cleanupPidFile kills whatever pid the file records at test cleanup. It reads
// the file lazily so the cleanup also works when the job started late.
func cleanupPidFile(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid > 1 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// cleanupEscapedSleep kills any process whose command line contains the given
// unique marker. Only used for jobs that left the session on purpose.
func cleanupEscapedSleep(t *testing.T, marker string) {
	t.Helper()
	t.Cleanup(func() {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return
		}
		self := os.Getpid()
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil || pid <= 1 || pid == self {
				continue
			}
			cmdline, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
			if err != nil || !strings.Contains(string(cmdline), marker) {
				continue
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// terminalResources is the agent's resource baseline, captured after a warm-up
// session so lazy runtime and gRPC state are not counted as a leak.
type terminalResources struct {
	fds        int
	ptmx       int
	goroutines int
}

// terminalResourceBaseline snapshots the agent's fd, PTY-master and goroutine
// counts.
func terminalResourceBaseline(t *testing.T) terminalResources {
	t.Helper()
	return terminalResources{fds: openFDCount(t), ptmx: ptmxFDCount(t), goroutines: runtime.NumGoroutine()}
}

// ptmxFDCount counts only PTY master descriptors, so the leak assertion is
// exact: one leaked master per session is a failure, while unrelated gRPC fds
// may still fluctuate.
func ptmxFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && strings.Contains(target, "ptmx") {
			count++
		}
	}
	return count
}

// waitForResources fails unless the agent's PTY-master descriptors return to
// their exact baseline and unrelated fds/goroutines stay within a small
// tolerance for gRPC bookkeeping.
func waitForResources(t *testing.T, baseline terminalResources, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var leakedPtmx, otherFD, goroutines int
	for time.Now().Before(deadline) {
		leakedPtmx = ptmxFDCount(t) - baseline.ptmx
		otherFD = openFDCount(t) - baseline.fds - leakedPtmx
		goroutines = runtime.NumGoroutine() - baseline.goroutines
		if leakedPtmx == 0 && otherFD <= 2 && goroutines <= 4 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("resources after close: leaked ptmx fds %d (baseline %d), other fds %d (baseline %d), goroutines +%d (baseline %d)",
		leakedPtmx, baseline.ptmx, otherFD, baseline.fds, goroutines, baseline.goroutines)
}

// runBackgroundJob starts line (which must background the job and write its pid
// to pidPath) and returns the pid.
func runBackgroundJob(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient, pidPath, command string) int {
	t.Helper()
	cleanupPidFile(t, pidPath)
	sendTerminalInput(t, stream, command+" & echo $! > "+pidPath+"\r")
	return waitForPID(t, pidPath)
}

// sessionLeaderPid reads the shell's own pid into pidPath.
func sessionLeaderPid(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient, pidPath string) int {
	t.Helper()
	cleanupPidFile(t, pidPath)
	sendTerminalInput(t, stream, "echo $$ > "+pidPath+"\r")
	return waitForPID(t, pidPath)
}

func TestTerminalCloseKillsJobControlBackgroundJob(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baseline := terminalResourceBaseline(t)

	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	shellPid := sessionLeaderPid(t, stream, terminalPidFile(root, "shell.pid"))
	pid := runBackgroundJob(t, stream, terminalPidFile(root, "job.pid"), "sleep 730")

	// An interactive shell with job control puts the job in its own process
	// group; that is the case the process-group-only cleanup missed.
	if group := terminalProcessGroup(t, pid); group != shellPid {
		t.Logf("background job %d runs in process group %d (shell %d): own group covered", pid, group, shellPid)
	}

	closeTerminalSession(t, stream)
	waitForProcessGone(t, pid)
	waitForResources(t, baseline, 8*time.Second)
}

func TestTerminalCloseKillsNonJobControlBackgroundJob(t *testing.T) {
	// A script run as the shell is non-interactive: no job control, so the
	// background job stays in the shell's own process group.
	pidPath := filepath.Join(t.TempDir(), "job.pid")
	script := filepath.Join(t.TempDir(), "noninteractive.sh")
	body := "#!/bin/sh\ntrap '' HUP\nsleep 730 &\necho $! > " + pidPath + "\nwhile :; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baseline := terminalResourceBaseline(t)
	terminal.shell = script

	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	pid := waitForPID(t, pidPath)
	cleanupPidFile(t, pidPath)

	closeTerminalSession(t, stream)
	waitForProcessGone(t, pid)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	waitForResources(t, baseline, 8*time.Second)
}

func TestTerminalCloseKillsNohupJob(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	pid := runBackgroundJob(t, stream, terminalPidFile(root, "nohup.pid"), "nohup sleep 731 >/dev/null 2>&1")

	closeTerminalSession(t, stream)
	waitForProcessGone(t, pid)
}

// TestTerminalCloseHardKillsHupTermIgnoringJob pins the SIGKILL sweep: the job
// ignores both SIGHUP and SIGTERM, so only the post-grace SIGKILL ends it.
func TestTerminalCloseHardKillsHupTermIgnoringJob(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	pid := runBackgroundJob(t, stream, terminalPidFile(root, "stubborn.pid"),
		"sh -c 'trap \"\" HUP TERM; sleep 732' >/dev/null 2>&1")

	started := time.Now()
	closeTerminalSession(t, stream)
	waitForProcessGone(t, pid)
	if elapsed := time.Since(started); elapsed < terminalKillGrace {
		t.Logf("stubborn job ended in %s", elapsed)
	}
}

// TestTerminalCloseReleasesMasterWithSetsidJob covers the escaped case: the job
// may survive (it left the session), but it must not pin the agent's master fd
// or its output-pump goroutine.
func TestTerminalCloseReleasesMasterWithSetsidJob(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid is not installed")
	}
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baseline := terminalResourceBaseline(t)

	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	marker := "733.733"
	cleanupEscapedSleep(t, marker)
	// `setsid` may fork to become a session leader, so the job is found by its
	// unique sleep argument at cleanup, never by the recorded $!.
	sendTerminalInput(t, stream, "setsid sleep "+marker+" & echo $! > "+terminalPidFile(root, "setsid.pid")+"\r")
	time.Sleep(300 * time.Millisecond)

	closeTerminalSession(t, stream)
	// The escaped job may still exist; the master fd and the pump goroutine must
	// not wait for it.
	waitForResources(t, baseline, 8*time.Second)
}

func TestTerminalCloseKillsNestedSubshellJob(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	shPidPath := terminalPidFile(root, "nested-sh.pid")
	sleepPidPath := terminalPidFile(root, "nested-sleep.pid")
	cleanupPidFile(t, shPidPath)
	cleanupPidFile(t, sleepPidPath)
	sendTerminalInput(t, stream, "sh -c 'sleep 734 & echo $! > "+sleepPidPath+"; wait' & echo $! > "+shPidPath+"\r")
	shPid := waitForPID(t, shPidPath)
	sleepPid := waitForPID(t, sleepPidPath)

	closeTerminalSession(t, stream)
	waitForProcessGone(t, shPid)
	waitForProcessGone(t, sleepPid)
}

// TestTerminalCloseAfterShellExitKillsJob covers a shell that exits while a
// background job keeps the slave open: the stream is still attached, then the
// client closes. The cleanup must not be skipped just because the shell is gone.
func TestTerminalCloseAfterShellExitKillsJob(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	pidPath := terminalPidFile(root, "orphan.pid")
	cleanupPidFile(t, pidPath)
	sendTerminalInput(t, stream, "sleep 735 & echo $! > "+pidPath+"; exit 0\r")
	pid := waitForPID(t, pidPath)

	// Give the shell time to exit and be reaped; the job still holds the slave.
	time.Sleep(500 * time.Millisecond)
	closeTerminalSession(t, stream)
	waitForProcessGone(t, pid)
}

func TestTerminalCloseDuringHeavyOutput(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baseline := terminalResourceBaseline(t)

	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	sendTerminalInput(t, stream, "while :; do echo gotham-flood; done\r")
	time.Sleep(200 * time.Millisecond)
	closeTerminalSession(t, stream)
	waitForResources(t, baseline, 8*time.Second)
}

// TestTerminalCloseCyclesReleaseResources runs bounded open/close cycles with a
// background job each time and asserts the agent returns to its baseline fd and
// goroutine counts.
func TestTerminalCloseCyclesReleaseResources(t *testing.T) {
	const cycles = 20
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baseline := terminalResourceBaseline(t)

	pids := make([]int, 0, cycles)
	for i := 0; i < cycles; i++ {
		stream := openTerminalSession(t, client, rootName)
		pidPath := terminalPidFile(root, fmt.Sprintf("cycle-%d.pid", i))
		pid := runBackgroundJob(t, stream, pidPath, "sleep 736")
		pids = append(pids, pid)
		closeTerminalSession(t, stream)
		_ = stream.CloseSend()
	}
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	for _, pid := range pids {
		waitForProcessGone(t, pid)
	}
	waitForResources(t, baseline, 10*time.Second)
}

// terminalProcessGroup reads field 5 (pgrp) of /proc/<pid>/stat.
func terminalProcessGroup(t *testing.T, pid int) int {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		t.Fatalf("read stat for %d: %v", pid, err)
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		t.Fatalf("no comm terminator in %q", data)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 3 {
		t.Fatalf("short stat for %d: %q", pid, data)
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		t.Fatalf("pgrp for %d: %v", pid, err)
	}
	return group
}

// newSessionLeader starts a real child in its own session so the /proc walk can
// be exercised without a PTY.
func newSessionLeader(t *testing.T, seconds string) int {
	t.Helper()
	cmd := exec.Command("sleep", seconds)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start session leader: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}

// TestTerminalSessionScanReportsTruncation pins the MEDIUM finding: a walk cut
// short by the process cap must report itself incomplete, never as a complete
// (possibly empty) session.
func TestTerminalSessionScanReportsTruncation(t *testing.T) {
	pid := newSessionLeader(t, "739")
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the session leader's start time")
	}
	previous := terminalScanLimit
	// A cap of zero makes the very first inspectable pid truncate the walk,
	// independent of how many processes the test host happens to have.
	terminalScanLimit = 0
	members, complete := terminalSessionMembers(pid, start, time.Now().Add(2*time.Second))
	terminalScanLimit = previous
	closeTerminalSessionMembers(members)
	if complete {
		t.Fatalf("walk truncated at cap %d reported complete (members %d)", 0, len(members))
	}
}

// TestTerminalSessionIncompleteScanWarns pins the operator-visible half of the
// MEDIUM finding: an incomplete walk is logged at WARN with the session id and
// the number of members found, and arms the process-group fallback.
func TestTerminalSessionIncompleteScanWarns(t *testing.T) {
	pid := newSessionLeader(t, "742")
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the session leader's start time")
	}
	var logs bytes.Buffer
	session := &terminalSession{
		sid:         pid,
		leaderStart: start,
		log:         slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
		done:        make(chan struct{}),
	}
	close(session.done) // the group fallback returns immediately; no signal is sent

	previous := terminalScanLimit
	terminalScanLimit = 0
	session.endSession()
	terminalScanLimit = previous

	message := logs.String()
	for _, want := range []string{"level=WARN", "session scan incomplete", "session_id=" + strconv.Itoa(pid)} {
		if !strings.Contains(message, want) {
			t.Fatalf("WARN log %q does not contain %q", message, want)
		}
	}
}

// TestTerminalSessionScanReportsDeadline pins the wall-clock cut: a deadline that
// has already passed is a truncated walk, not an empty session.
func TestTerminalSessionScanReportsDeadline(t *testing.T) {
	pid := newSessionLeader(t, "740")
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the session leader's start time")
	}
	members, complete := terminalSessionMembers(pid, start, time.Now().Add(-time.Second))
	closeTerminalSessionMembers(members)
	if complete {
		t.Fatal("deadline-truncated walk reported complete")
	}
}

// TestTerminalSessionScanUsesPidfd checks that a verified member is bound to a
// pidfd before it is signalled. It skips on kernels or sandboxes without pidfds.
func TestTerminalSessionScanUsesPidfd(t *testing.T) {
	maskHiddenProcEntries(t)
	pid := newSessionLeader(t, "741")
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the session leader's start time")
	}
	probe, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Skipf("pidfds unavailable on this kernel/sandbox: %v", err)
	}
	_ = unix.Close(probe)

	members, complete := terminalSessionMembers(pid, start, time.Now().Add(2*time.Second))
	defer closeTerminalSessionMembers(members)
	if !complete {
		t.Fatal("complete scan reported incomplete")
	}
	if len(members) != 1 || members[0].pid != pid {
		t.Fatalf("members = %+v, want exactly pid %d", members, pid)
	}
	if members[0].fd < 0 {
		t.Fatal("member was not bound to a pidfd")
	}
}

// maskHiddenProcEntries makes the scan treat entries hidden by a hardened /proc
// (hidepid, as on the CI runner) as processes that exited. Tests that need a
// complete walk stay deterministic on such hosts; the production behaviour under
// hidepid is covered by the incomplete-scan tests.
func maskHiddenProcEntries(t *testing.T) {
	t.Helper()
	previous := terminalScanStat
	terminalScanStat = func(pid int) (terminalStat, error) {
		stat, err := previous(pid)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return terminalStat{}, fs.ErrNotExist
		}
		return stat, err
	}
	t.Cleanup(func() { terminalScanStat = previous })
}

// newStubbornSessionLeader starts a real shell that ignores HUP and TERM in its
// own session, so only SIGKILL can end it.
func newStubbornSessionLeader(t *testing.T, seconds string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "trap '' HUP TERM; sleep "+seconds)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stubborn session leader: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return pid
}

// TestTerminalCloseKillsStubbornJobWhenARescanIsIncomplete is the round-2
// regression: the first scan is complete and keeps the job's pidfd, then every
// later scan is truncated. The kept member must still be SIGKILLed; without that
// the job in its own process group survives.
func TestTerminalCloseKillsStubbornJobWhenARescanIsIncomplete(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	pid := runBackgroundJob(t, stream, terminalPidFile(root, "stubborn-incomplete.pid"),
		"sh -c 'trap \"\" HUP TERM; sleep 748' >/dev/null 2>&1")

	previous := terminalScanLimit
	defer func() { terminalScanLimit = previous }()
	closeTerminalSession(t, stream)
	// The first scan runs immediately; the first grace probe fires at
	// terminalGraceProbe (500 ms). Truncate every re-scan in between.
	time.Sleep(300 * time.Millisecond)
	terminalScanLimit = 0

	waitForProcessGone(t, pid)
}

// TestTerminalEndSessionKillsFoundMembersWhenFirstScanIncomplete covers the
// other half: the very first walk is incomplete (an unreadable /proc entry) but
// still found the stubborn member. It must be SIGTERMed now and SIGKILLed after
// the grace instead of only falling back to the shell's process group.
func TestTerminalEndSessionKillsFoundMembersWhenFirstScanIncomplete(t *testing.T) {
	pid := newStubbornSessionLeader(t, "749")
	decoy := newSessionLeader(t, "750")

	previous := terminalScanStat
	terminalScanStat = func(candidate int) (terminalStat, error) {
		if candidate == decoy {
			return terminalStat{}, fs.ErrPermission // simulate hidepid on one entry
		}
		return readTerminalStatErr(candidate)
	}
	defer func() { terminalScanStat = previous }()

	session := &terminalSession{
		sid:         pid,
		leaderStart: terminalProcessStartTime(pid),
		log:         discardLogger(),
		done:        make(chan struct{}),
	}
	close(session.done) // the group fallback is a no-op: the member SIGKILL must act

	session.endSession()
	waitForProcessGone(t, pid)
}

// TestTerminalSessionMembersRejectsReusedPidAfterPidfdOpen pins the pidfd
// identity re-check. A fake verifier that reports a different start time (or a
// different session) must drop the member, so no signal is ever sent; removing
// the re-check makes this test fail.
func TestTerminalSessionMembersRejectsReusedPidAfterPidfdOpen(t *testing.T) {
	maskHiddenProcEntries(t)
	pid := newSessionLeader(t, "751")
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the session leader's start time")
	}
	probe, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Skipf("pidfds unavailable on this kernel/sandbox: %v", err)
	}
	_ = unix.Close(probe)

	previous := terminalVerifyStat
	defer func() { terminalVerifyStat = previous }()
	cases := []struct {
		name string
		stat terminalStat
	}{
		{"different start time", terminalStat{pid: pid, state: 'S', session: pid, startTime: start + 1}},
		{"different session", terminalStat{pid: pid, state: 'S', session: pid + 1, startTime: start}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			terminalVerifyStat = func(candidate int) (terminalStat, error) {
				if candidate == pid {
					return testCase.stat, nil
				}
				return previous(candidate)
			}
			members, complete := terminalSessionMembers(pid, start, time.Now().Add(2*time.Second))
			closeTerminalSessionMembers(members)
			if !complete {
				t.Fatal("scan reported incomplete")
			}
			if len(members) != 0 {
				t.Fatalf("reused pid was kept: %+v", members)
			}
		})
	}

	// The matching record keeps the member.
	terminalVerifyStat = previous
	members, complete := terminalSessionMembers(pid, start, time.Now().Add(2*time.Second))
	closeTerminalSessionMembers(members)
	if !complete || len(members) != 1 {
		t.Fatalf("matching member: complete=%v members=%+v", complete, members)
	}
}

// TestTerminalSessionScansAreBounded proves the process-wide scan slot: while
// every slot is held, many concurrent walks give up at their deadline instead of
// adding unbounded /proc scans.
func TestTerminalSessionScansAreBounded(t *testing.T) {
	pid := newSessionLeader(t, "752")
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the session leader's start time")
	}
	for index := 0; index < cap(terminalScanSlots); index++ {
		terminalScanSlots <- struct{}{}
	}
	defer func() {
		for index := 0; index < cap(terminalScanSlots); index++ {
			<-terminalScanSlots
		}
	}()

	var wg sync.WaitGroup
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			members, complete := terminalSessionMembers(pid, start, time.Now().Add(100*time.Millisecond))
			closeTerminalSessionMembers(members)
			if complete {
				t.Error("scan reported complete while every scan slot was held")
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("20 waiting scans did not return within 5 s")
	}
}

// TestParseTerminalStat pins the parser against a comm field that contains
// spaces and parentheses, which is why parsing starts after the last ')'.
func TestParseTerminalStat(t *testing.T) {
	record := []byte("1234 (my weird) (comm)\nname) S 1 1234 4321 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 987654 0 0")
	stat, ok := parseTerminalStat(1234, record)
	if !ok {
		t.Fatalf("parseTerminalStat refused %q", record)
	}
	if stat.pid != 1234 || stat.state != 'S' || stat.session != 4321 || stat.startTime != 987654 {
		t.Fatalf("stat = %+v, want pid 1234 state S session 4321 start 987654", stat)
	}
	for _, invalid := range [][]byte{
		[]byte(""),
		[]byte("1234 (unterminated"),
		[]byte("1234 (x) S 1 1234"),
		[]byte("1234 (x) S 1 1234 0 0 -1 4194304 0 0 0 0 0 0 0 0 0 20 0 1 0 not-a-number"),
	} {
		if _, ok := parseTerminalStat(1234, invalid); ok {
			t.Fatalf("parseTerminalStat accepted %q", invalid)
		}
	}
}

// TestTerminalSessionMembersRefusesReusedLeader pins the pid-reuse guard: a
// process that owns the recorded pid with a different start time is a different
// session, and the scan must refuse to signal its members.
func TestTerminalSessionMembersRefusesReusedLeader(t *testing.T) {
	maskHiddenProcEntries(t)
	cmd := exec.Command("sleep", "737")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	pid := cmd.Process.Pid
	start := terminalProcessStartTime(pid)
	if start == 0 {
		t.Fatal("could not read the child's start time")
	}
	if members, ok := terminalSessionMembers(pid, start+1, time.Now().Add(time.Second)); ok {
		closeTerminalSessionMembers(members)
		t.Fatal("scan accepted a session leader with a mismatched start time")
	}
	// The same leader start time is accepted; the child is not a session leader,
	// so it simply contributes no members.
	members, ok := terminalSessionMembers(pid, start, time.Now().Add(time.Second))
	closeTerminalSessionMembers(members)
	if !ok {
		t.Fatal("scan refused a matching session leader")
	}
	if members, ok := terminalSessionMembers(1, start, time.Now().Add(time.Second)); ok || len(members) != 0 {
		closeTerminalSessionMembers(members)
		t.Fatalf("scan accepted pid 1 as a session: %v, %v", members, ok)
	}
}

// TestTerminalNonblockingMaster checks that the master's O_NONBLOCK flag is set
// after a real session starts, so Close can interrupt a pending Read.
func TestTerminalNonblockingMaster(t *testing.T) {
	cmd := exec.Command("sleep", "738")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	master, err := startTerminalPTY(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	raw, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	if err := raw.Control(func(fd uintptr) {
		value, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		if errno != 0 {
			t.Fatalf("fcntl: %v", errno)
		}
		flags = int(value)
	}); err != nil {
		t.Fatal(err)
	}
	if flags&syscall.O_NONBLOCK == 0 {
		t.Fatalf("master flags = %#o, want O_NONBLOCK set", flags)
	}
	_ = master.Close()
}

// startTerminalPTY is the production start path without a TerminalServer.
func startTerminalPTY(cmd *exec.Cmd) (*os.File, error) {
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return nil, err
	}
	if err := terminalNonblock(master); err != nil {
		_ = master.Close()
		return nil, err
	}
	return master, nil
}
