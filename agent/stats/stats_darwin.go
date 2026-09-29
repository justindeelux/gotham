//go:build darwin

package stats

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// readCPUTimes is unsupported on Darwin. macOS exposes CPU load counters only
// through Mach host_statistics, which golang.org/x/sys/unix does not wrap.
// Reporting zero keeps the heartbeat healthy; Linux is the production target.
func readCPUTimes() (idle, total uint64, err error) {
	return 0, 0, errUnsupported
}

// readMemory reports used and total bytes, using hw.memsize for the total and
// the reclaimable page pools for the free estimate.
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
		// The total is still useful for registration.
		return 0, total, nil
	}
	if available > total {
		available = total
	}
	return total - available, total, nil
}

// availableMemory sums the immediately reclaimable page pools. Darwin keeps
// most of RAM in the file cache, so this is a rough approximation.
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

// osVersion reads the kernel release, for example "24.6.0".
func osVersion() string {
	release, err := unix.Sysctl("kern.osrelease")
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(release), "\x00")
}

// readNetwork is unsupported on Darwin: per-interface byte counters live in the
// Mach interface statistics, which golang.org/x/sys/unix does not wrap. The
// heartbeat reports zero, matching readCPUTimes. Linux is the production
// target.
func readNetwork() (rx, tx uint64, err error) {
	return 0, 0, errUnsupported
}

// readDiskIO is unsupported on Darwin for the same reason as readNetwork.
func readDiskIO() (read, write uint64, err error) {
	return 0, 0, errUnsupported
}
