//go:build !unix

package terminal

import "os"

var (
	terminalHangup    os.Signal = os.Interrupt
	terminalTerminate os.Signal = os.Interrupt
	terminalKill      os.Signal = os.Kill
)

func killTerminalProcessGroup(pid int, signal os.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

func terminalProcessStatus(state *os.ProcessState) (int32, string) {
	if state == nil {
		return -1, ""
	}
	return int32(state.ExitCode()), ""
}
