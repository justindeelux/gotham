//go:build linux

package terminal

import (
	"os"
	"syscall"
)

// terminalNonblock makes the PTY master non-blocking without going through
// os.File.Fd. Fd leaves the descriptor in blocking mode, and creack/pty's own
// ioctl helpers call it while the session starts, so the flag has to be
// restored afterwards. A non-blocking, pollable master lets Close interrupt a
// pending Read even when a process outside the session still holds the slave
// open; otherwise the kernel keeps the read in flight and the master fd stays
// pinned until that process exits.
//
// Linux only: on other platforms Go does not register a pty master with its
// poller, so clearing the flag would surface EAGAIN to the caller instead of
// waiting. The supported agent target is Linux.
func terminalNonblock(master *os.File) error {
	raw, err := master.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if err := raw.Control(func(fd uintptr) {
		setErr = syscall.SetNonblock(int(fd), true)
	}); err != nil {
		return err
	}
	return setErr
}
