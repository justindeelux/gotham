//go:build linux

package deploy

import (
	"os/exec"
	"runtime"
	"syscall"
)

func configureGitCommand(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = gitWaitDelay
	runtime.LockOSThread()
	return runtime.UnlockOSThread
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
