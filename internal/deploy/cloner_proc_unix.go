//go:build unix && !linux

package deploy

import (
	"os/exec"
	"syscall"
)

// configureGitCommand isolates a git child on the non-Linux Unixes (the
// developer platform): its own process group with a group kill on
// cancellation, so git and the sh/ssh it spawned die together. There is no
// parent-death signal here, so a hard death of the control plane is not
// covered on this platform.
func configureGitCommand(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = gitWaitDelay
	return func() {}
}

// killProcessGroup SIGKILLs the child's whole process group (negative pid).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
