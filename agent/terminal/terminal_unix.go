//go:build unix

package terminal

import (
	"os"
	"syscall"
)

// The signals a terminal session uses: a polite hangup first, then a hard kill
// after the grace period.
var (
	terminalHangup    os.Signal = syscall.SIGHUP
	terminalTerminate os.Signal = syscall.SIGTERM
	terminalKill      os.Signal = syscall.SIGKILL
)

// killTerminalProcessGroup signals the whole process group led by pid, so a
// shell's children (a running build, an editor, a pager) die with it. The PTY
// child is a session leader because pty.StartWithSize sets Setsid, so a
// negative pid addresses exactly that group.
func killTerminalProcessGroup(pid int, signal os.Signal) error {
	if pid <= 0 {
		return nil
	}
	unixSignal, ok := signal.(syscall.Signal)
	if !ok {
		return nil
	}
	return syscall.Kill(-pid, unixSignal)
}

// terminalProcessStatus reports the child's exit code and terminating signal.
func terminalProcessStatus(state *os.ProcessState) (int32, string) {
	if state == nil {
		return -1, ""
	}
	if wait, ok := state.Sys().(syscall.WaitStatus); ok && wait.Signaled() {
		return -1, wait.Signal().String()
	}
	return int32(state.ExitCode()), ""
}
