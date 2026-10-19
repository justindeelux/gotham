//go:build !unix

package agent

import "os"

// Non-unix hosts are not a supported agent target. These stubs keep the package
// buildable for tooling; a session on such a host fails at StartWithSize.
var (
	terminalHangup os.Signal = os.Interrupt
	terminalKill   os.Signal = os.Kill
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
