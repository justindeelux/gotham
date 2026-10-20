//go:build !linux

package agent

import "os"

// terminalNonblock keeps the host PTY master in its original blocking mode.
// Go does not register a pty master with the poller on these platforms (on
// darwin kqueue registration fails), so a non-blocking descriptor would hand
// EAGAIN to the reader rather than wait for readiness. The supported agent
// target is Linux, where the master is made pollable.
func terminalNonblock(master *os.File) error { return nil }
