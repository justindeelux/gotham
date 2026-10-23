//go:build unix && !linux

package deploy

import (
	"os/exec"
	"syscall"
)

func configureGitCommand(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = gitWaitDelay
	return func() {}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
