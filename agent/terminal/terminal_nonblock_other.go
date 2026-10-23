//go:build !linux

package terminal

import "os"

func terminalNonblock(master *os.File) error { return nil }
