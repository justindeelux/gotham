//go:build linux

package terminal

import (
	"os"
	"syscall"
)

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
