//go:build darwin

package stats

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func readCPUTimes() (idle, total uint64, err error) {
	return 0, 0, errUnsupported
}

func readMemory() (used, total uint64, err error) {
	total, err = unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, 0, err
	}
	if total == 0 {
		return 0, 0, errUnsupported
	}
	available, availableErr := availableMemory()
	if availableErr != nil {
		return 0, total, nil
	}
	if available > total {
		available = total
	}
	return total - available, total, nil
}

func availableMemory() (uint64, error) {
	var pages uint64
	for _, key := range []string{
		"vm.page_free_count",
		"vm.page_speculative_count",
		"vm.page_purgeable_count",
	} {
		count, err := unix.SysctlUint32(key)
		if err != nil {
			continue
		}
		pages += uint64(count)
	}
	if pages == 0 {
		return 0, errUnsupported
	}
	return pages * uint64(os.Getpagesize()), nil
}

func osVersion() string {
	release, err := unix.Sysctl("kern.osrelease")
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(release), "\x00")
}

func readNetwork() (rx, tx uint64, err error) {
	return 0, 0, errUnsupported
}

func readDiskIO() (read, write uint64, err error) {
	return 0, 0, errUnsupported
}
