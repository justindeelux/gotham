// Package stats samples host resource usage for the Gotham node agent.
//
// It deliberately depends only on the standard library and golang.org/x/sys so
// the agent can be built as a standalone binary that never imports gotham
// internal packages. Every metric is best-effort: a metric that cannot be read
// is reported as zero rather than failing the heartbeat.
package stats

import (
	"errors"
	"math"
	"sync"
	"syscall"
)

// Usage is a point-in-time sample of host resource usage. CPU, Mem and Disk are
// fractions in the range 0..1. TotalMem and TotalDisk are byte counts.
type Usage struct {
	CPU       float64
	Mem       float64
	Disk      float64
	TotalMem  uint64
	TotalDisk uint64
}

// errUnsupported is returned by platform hooks that cannot provide a metric.
var errUnsupported = errors.New("stats: metric unsupported on this platform")

// cpuTimes holds a CPU counter sample: cumulative idle and total jiffies.
type cpuTimes struct {
	idle  uint64
	total uint64
}

// Sampler computes CPU usage from the delta between consecutive samples. Create
// one sampler and reuse it: the first sample reports zero CPU because there is
// no previous counter to compare against.
type Sampler struct {
	mu      sync.Mutex
	lastCPU cpuTimes
	haveCPU bool
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
