//go:build linux

package deploy

import (
	"os/exec"
	"runtime"
	"syscall"
)

// configureGitCommand isolates a git child on Linux. The unit keeps
// KillMode=process (the privileged update wrapper must survive the restart
// cgroup), so systemd never kills control-plane children itself; the cloner
// must do it:
//
//   - Setpgid puts git in its own process group and Cancel kills the whole
//     group, so a cancelled clone (step timeout, deploy cancel, graceful
//     service shutdown) takes down git and the sh/ssh it spawned. A plain
//     Process.Kill would leave the grandchildren running.
//   - Pdeathsig SIGKILLs git when the thread that forked it dies, which covers
//     a hard SIGKILL (or OOM) of the control plane where no Go code gets to run.
//     The signal is delivered when that *thread* exits, and the Go runtime may
//     retire an unlocked thread before the child is done, so the caller's
//     goroutine is locked to its thread for the child's lifetime.
//
// A grandchild that leaves the process group (a process that calls setsid
// itself) is not covered after a hard kill; the complete answer is the service
// cgroup with KillMode=control-group and the wrapper in a transient scope
// (see deploy/README.md, Known residuals).
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

// killProcessGroup SIGKILLs the child's whole process group (negative pid):
// git, the sh it runs GIT_SSH_COMMAND through, and ssh. Wait still reaps the
// direct child; ESRCH just means the group is already gone.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
