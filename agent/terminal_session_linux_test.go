//go:build linux

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

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

// waitForResources fails unless the agent's fd and goroutine counts return to
// the baseline (with a small tolerance for gRPC bookkeeping) inside the bound.
func waitForResources(t *testing.T, baselineFD, baselineGoroutines int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if openFDCount(t) <= baselineFD+2 && runtime.NumGoroutine() <= baselineGoroutines+4 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("resources after close: fds %d (baseline %d), goroutines %d (baseline %d)",
		openFDCount(t), baselineFD, runtime.NumGoroutine(), baselineGoroutines)
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
	baselineFD, baselineGoroutines := openFDCount(t), runtime.NumGoroutine()

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
	waitForResources(t, baselineFD, baselineGoroutines, 8*time.Second)
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
	baselineFD, baselineGoroutines := openFDCount(t), runtime.NumGoroutine()
	terminal.shell = script

	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	pid := waitForPID(t, pidPath)
	cleanupPidFile(t, pidPath)

	closeTerminalSession(t, stream)
	waitForProcessGone(t, pid)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	waitForResources(t, baselineFD, baselineGoroutines, 8*time.Second)
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
	baselineFD, baselineGoroutines := openFDCount(t), runtime.NumGoroutine()

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
	waitForResources(t, baselineFD, baselineGoroutines, 8*time.Second)
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
	baselineFD, baselineGoroutines := openFDCount(t), runtime.NumGoroutine()

	stream := openTerminalSession(t, client, rootName)
	defer func() { _ = stream.CloseSend() }()
	sendTerminalInput(t, stream, "while :; do echo gotham-flood; done\r")
	time.Sleep(200 * time.Millisecond)
	closeTerminalSession(t, stream)
	waitForResources(t, baselineFD, baselineGoroutines, 8*time.Second)
}

// TestTerminalCloseCyclesReleaseResources runs bounded open/close cycles with a
// background job each time and asserts the agent returns to its baseline fd and
// goroutine counts.
func TestTerminalCloseCyclesReleaseResources(t *testing.T) {
	const cycles = 20
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baselineFD, baselineGoroutines := openFDCount(t), runtime.NumGoroutine()

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
	waitForResources(t, baselineFD, baselineGoroutines, 10*time.Second)
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
	if _, ok := terminalSessionMembers(pid, start+1, time.Now().Add(time.Second)); ok {
		t.Fatal("scan accepted a session leader with a mismatched start time")
	}
	// The same leader start time is accepted; the child is not a session leader,
	// so it simply contributes no members.
	if _, ok := terminalSessionMembers(pid, start, time.Now().Add(time.Second)); !ok {
		t.Fatal("scan refused a matching session leader")
	}
	if members, ok := terminalSessionMembers(1, start, time.Now().Add(time.Second)); ok || len(members) != 0 {
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
