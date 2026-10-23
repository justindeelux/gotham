//go:build unix

package terminal

import (
	"os"
	"syscall"
)

var (
	terminalHangup    os.Signal = syscall.SIGHUP
	terminalTerminate os.Signal = syscall.SIGTERM
	terminalKill      os.Signal = syscall.SIGKILL
)

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

func terminalProcessStatus(state *os.ProcessState) (int32, string) {
	if state == nil {
		return -1, ""
	}
	if wait, ok := state.Sys().(syscall.WaitStatus); ok && wait.Signaled() {
		return -1, wait.Signal().String()
	}
	return int32(state.ExitCode()), ""
}
