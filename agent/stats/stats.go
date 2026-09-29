// Package stats samples host resource usage for the Gotham node agent.
//
// It deliberately depends only on the standard library and golang.org/x/sys so
// the agent can be built as a standalone binary that never imports gotham
// internal packages. Every metric is best-effort: a metric that cannot be read
// is reported as zero rather than failing the heartbeat.
package stats

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Usage is a point-in-time sample of host resource usage. CPU, Mem and Disk are
// fractions in the range 0..1. The *Bps fields are byte-per-second rates
// measured between consecutive samples; they are zero on the first sample and
// on a platform that cannot report them. TotalMem and TotalDisk are byte
// counts.
type Usage struct {
	CPU          float64
	Mem          float64
	Disk         float64
	NetRxBps     float64
	NetTxBps     float64
	DiskReadBps  float64
	DiskWriteBps float64
	TotalMem     uint64
	TotalDisk    uint64
}

// errUnsupported is returned by platform hooks that cannot provide a metric.
var errUnsupported = errors.New("stats: metric unsupported on this platform")

// cpuTimes holds a CPU counter sample: cumulative idle and total jiffies.
type cpuTimes struct {
	idle  uint64
	total uint64
}

// counterPair is one I/O source's cumulative byte counters together with the
// instant they were read, so a rate is only ever computed from two consecutive
// reads of the same source.
type counterPair struct {
	in    uint64
	out   uint64
	at    time.Time
	valid bool
}

// rates derives the byte-per-second rates from the previous read of the same
// source. Without a previous read both rates are zero: the first sample only
// establishes the baseline.
func (c counterPair) rates(previous counterPair) (in, out float64) {
	if !previous.valid {
		return 0, 0
	}
	elapsed := c.at.Sub(previous.at)
	return byteRate(c.in, previous.in, elapsed), byteRate(c.out, previous.out, elapsed)
}

// byteRate converts the delta of two cumulative byte counters over elapsed into
// a byte-per-second rate. A counter that moved backwards (a reset or a
// replaced device) or a non-positive interval reports zero for that interval
// instead of a bogus spike.
func byteRate(current, previous uint64, elapsed time.Duration) float64 {
	if elapsed <= 0 || current < previous {
		return 0
	}
	return float64(current-previous) / elapsed.Seconds()
}

// Sampler computes CPU usage and I/O rates from the delta between consecutive
// samples. Create one sampler and reuse it: the first sample reports zero for
// every rate because there is no previous counter to compare against.
type Sampler struct {
	mu       sync.Mutex
	lastCPU  cpuTimes
	haveCPU  bool
	lastNet  counterPair
	lastDisk counterPair
}

// New returns a ready-to-use Sampler.
func New() *Sampler { return &Sampler{} }

// Sample returns the current resource usage. Metrics that are unavailable are
// zero. The returned error is non-nil only when no metric could be read.
func (s *Sampler) Sample() (Usage, error) {
	var (
		usage   Usage
		read    bool
		lastErr error
	)

	if idle, total, err := readCPUTimes(); err != nil {
		lastErr = err
	} else {
		s.mu.Lock()
		if s.haveCPU && total > s.lastCPU.total && idle >= s.lastCPU.idle {
			totalDelta := float64(total - s.lastCPU.total)
			idleDelta := float64(idle - s.lastCPU.idle)
			usage.CPU = clampFraction((totalDelta - idleDelta) / totalDelta)
		}
		s.lastCPU = cpuTimes{idle: idle, total: total}
		s.haveCPU = true
		s.mu.Unlock()
		read = true
	}

	if used, total, err := readMemory(); err != nil {
		lastErr = err
	} else if total > 0 {
		usage.TotalMem = total
		usage.Mem = clampFraction(float64(used) / float64(total))
		read = true
	}

	if used, total, err := readDisk("/"); err != nil {
		lastErr = err
	} else if total > 0 {
		usage.TotalDisk = total
		usage.Disk = clampFraction(float64(used) / float64(total))
		read = true
	}

	rx, tx, netErr := readNetwork()
	diskRead, diskWrite, diskIOErr := readDiskIO()

	s.mu.Lock()
	now := time.Now()
	if netErr == nil {
		current := counterPair{in: rx, out: tx, at: now, valid: true}
		usage.NetRxBps, usage.NetTxBps = current.rates(s.lastNet)
		s.lastNet = current
		read = true
	}
	if diskIOErr == nil {
		current := counterPair{in: diskRead, out: diskWrite, at: now, valid: true}
		usage.DiskReadBps, usage.DiskWriteBps = current.rates(s.lastDisk)
		s.lastDisk = current
		read = true
	}
	s.mu.Unlock()

	if netErr != nil {
		lastErr = netErr
	}
	if diskIOErr != nil {
		lastErr = diskIOErr
	}

	if !read {
		if lastErr == nil {
			lastErr = errUnsupported
		}
		return Usage{}, lastErr
	}
	return usage, nil
}

// OSVersion returns a human-readable OS release string (for example
// "6.1.0-13-amd64") or "" when it cannot be determined.
func OSVersion() string { return osVersion() }

// TotalMemory returns the total physical memory in bytes, or 0 when unknown.
func TotalMemory() uint64 {
	_, total, err := readMemory()
	if err != nil {
		return 0
	}
	return total
}

// TotalDisk returns the total size of the filesystem mounted at path in bytes,
// or 0 when unknown.
func TotalDisk(path string) uint64 {
	_, total, err := readDisk(path)
	if err != nil {
		return 0
	}
	return total
}

// readDisk reports the used and total bytes of the filesystem at path.
func readDisk(path string) (used, total uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stat.Bsize)
	total = stat.Blocks * blockSize
	free := stat.Bavail * blockSize
	if free > total {
		free = total
	}
	return total - free, total, nil
}

// parseNetDev sums the bytes received and transmitted by every interface in a
// /proc/net/dev document. The loopback device is skipped: loopback traffic
// never leaves the host and would otherwise sit permanently on the charts.
func parseNetDev(r io.Reader) (rx, tx uint64, err error) {
	scanner := bufio.NewScanner(r)
	seen := false
	for scanner.Scan() {
		name, counters, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue // the two header lines have no colon
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(counters)
		// rx_bytes and tx_bytes are the 1st and 9th counter.
		if len(fields) < 9 {
			return 0, 0, fmt.Errorf("stats: malformed /proc/net/dev line for %q", name)
		}
		received, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("stats: /proc/net/dev rx bytes for %q: %w", name, err)
		}
		transmitted, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("stats: /proc/net/dev tx bytes for %q: %w", name, err)
		}
		rx += received
		tx += transmitted
		seen = true
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if !seen {
		return 0, 0, errors.New("stats: no interfaces in /proc/net/dev")
	}
	return rx, tx, nil
}

// sectorSize is the unit of the read/written counters in /proc/diskstats.
const sectorSize = 512

// parseDiskStats sums the bytes read and written by the devices in devices, a
// set of whole-device names. Partition lines are not part of such a set, so the
// same I/O is never counted twice through a whole disk and its partition.
func parseDiskStats(r io.Reader, devices map[string]struct{}) (read, write uint64, err error) {
	scanner := bufio.NewScanner(r)
	seen := false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// major minor name reads_read reads_merged sectors_read ... writes_written
		if len(fields) < 10 {
			continue
		}
		if _, ok := devices[fields[2]]; !ok {
			continue
		}
		sectorsRead, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("stats: /proc/diskstats sectors read for %q: %w", fields[2], err)
		}
		sectorsWritten, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("stats: /proc/diskstats sectors written for %q: %w", fields[2], err)
		}
		read += sectorsRead * sectorSize
		write += sectorsWritten * sectorSize
		seen = true
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if !seen {
		return 0, 0, errors.New("stats: no whole devices in /proc/diskstats")
	}
	return read, write, nil
}

// clampFraction constrains v to the 0..1 range, mapping NaN to 0.
func clampFraction(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
