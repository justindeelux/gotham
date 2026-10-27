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

// MetricStatus reports whether a value was measured in this sample. A measured
// zero is MetricOK; every other status means the value must not be shown as a
// zero measurement.
type MetricStatus string

const (
	MetricOK          MetricStatus = "ok"
	MetricUnsupported MetricStatus = "unsupported"
	MetricError       MetricStatus = "error"
	MetricWarming     MetricStatus = "warming"
)

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

	// Status/reason per metric. CPU, memory and disk usage are sampled
	// directly. The two I/O pairs share the status of their read, and start as
	// MetricWarming on the first counter sample instead of reporting zero.
	CPUStatus    MetricStatus
	CPUReason    string
	MemStatus    MetricStatus
	MemReason    string
	DiskStatus   MetricStatus
	DiskReason   string
	NetStatus    MetricStatus
	NetReason    string
	DiskIOStatus MetricStatus
	DiskIOReason string

	ObservedAt time.Time
}

var errUnsupported = errors.New("stats: metric unsupported on this platform")

type cpuTimes struct {
	idle  uint64
	total uint64
}

type counterPair struct {
	in    uint64
	out   uint64
	at    time.Time
	valid bool
}

func (c counterPair) rates(previous counterPair) (in, out float64) {
	if !previous.valid {
		return 0, 0
	}
	elapsed := c.at.Sub(previous.at)
	return byteRate(c.in, previous.in, elapsed), byteRate(c.out, previous.out, elapsed)
}

func (c counterPair) reset(previous counterPair) bool {
	return previous.valid && (c.in < previous.in || c.out < previous.out)
}

func byteRate(current, previous uint64, elapsed time.Duration) float64 {
	if elapsed <= 0 || current < previous {
		return 0
	}
	return float64(current-previous) / elapsed.Seconds()
}

type Sampler struct {
	mu       sync.Mutex
	reads    samplerReads
	now      func() time.Time
	lastCPU  cpuTimes
	haveCPU  bool
	lastNet  counterPair
	lastDisk counterPair
}

// samplerReads lets a test drive real sampler state with controlled counters;
// production uses the platform readers below.
type samplerReads struct {
	readCPU     func() (idle, total uint64, err error)
	readMemory  func() (used, total uint64, err error)
	readDisk    func(path string) (used, total uint64, err error)
	readNetwork func() (rx, tx uint64, err error)
	readDiskIO  func() (read, write uint64, err error)
}

func defaultReads() samplerReads {
	return samplerReads{
		readCPU:     readCPUTimes,
		readMemory:  readMemory,
		readDisk:    readDisk,
		readNetwork: readNetwork,
		readDiskIO:  readDiskIO,
	}
}

func New() *Sampler { return newSampler(defaultReads(), time.Now) }

func newSampler(reads samplerReads, now func() time.Time) *Sampler {
	return &Sampler{reads: reads, now: now}
}

func (s *Sampler) Sample() (Usage, error) {
	var (
		usage   Usage
		read    bool
		lastErr error
	)
	usage.ObservedAt = s.now()

	if idle, total, err := s.reads.readCPU(); err != nil {
		usage.CPUStatus, usage.CPUReason = statusFor(err)
		lastErr = err
	} else {
		s.mu.Lock()
		if s.haveCPU && total > s.lastCPU.total && idle >= s.lastCPU.idle {
			totalDelta := float64(total - s.lastCPU.total)
			idleDelta := float64(idle - s.lastCPU.idle)
			usage.CPU = clampFraction((totalDelta - idleDelta) / totalDelta)
			usage.CPUStatus = MetricOK
		} else if s.haveCPU {
			usage.CPUStatus = MetricWarming
			usage.CPUReason = "no_delta"
		} else {
			usage.CPUStatus = MetricWarming
			usage.CPUReason = "first_sample"
		}
		s.lastCPU = cpuTimes{idle: idle, total: total}
		s.haveCPU = true
		s.mu.Unlock()
		read = true
	}

	if used, total, err := s.reads.readMemory(); err != nil {
		usage.MemStatus, usage.MemReason = statusFor(err)
		lastErr = err
	} else if total > 0 {
		usage.TotalMem = total
		usage.Mem = clampFraction(float64(used) / float64(total))
		usage.MemStatus = MetricOK
		read = true
	} else {
		usage.MemStatus, usage.MemReason = MetricError, "empty"
	}

	if used, total, err := s.reads.readDisk("/"); err != nil {
		usage.DiskStatus, usage.DiskReason = statusFor(err)
		lastErr = err
	} else if total > 0 {
		usage.TotalDisk = total
		usage.Disk = clampFraction(float64(used) / float64(total))
		usage.DiskStatus = MetricOK
		read = true
	} else {
		usage.DiskStatus, usage.DiskReason = MetricError, "empty"
	}

	rx, tx, netErr := s.reads.readNetwork()
	diskRead, diskWrite, diskIOErr := s.reads.readDiskIO()

	s.mu.Lock()
	now := s.now()
	if netErr == nil {
		current := counterPair{in: rx, out: tx, at: now, valid: true}
		if current.reset(s.lastNet) {
			usage.NetStatus, usage.NetReason = MetricError, "counter_reset"
		} else if !s.lastNet.valid {
			usage.NetStatus, usage.NetReason = MetricWarming, "first_sample"
		} else {
			usage.NetStatus = MetricOK
		}
		usage.NetRxBps, usage.NetTxBps = current.rates(s.lastNet)
		s.lastNet = current
		read = true
	} else {
		usage.NetStatus, usage.NetReason = statusFor(netErr)
	}
	if diskIOErr == nil {
		current := counterPair{in: diskRead, out: diskWrite, at: now, valid: true}
		if current.reset(s.lastDisk) {
			usage.DiskIOStatus, usage.DiskIOReason = MetricError, "counter_reset"
		} else if !s.lastDisk.valid {
			usage.DiskIOStatus, usage.DiskIOReason = MetricWarming, "first_sample"
		} else {
			usage.DiskIOStatus = MetricOK
		}
		usage.DiskReadBps, usage.DiskWriteBps = current.rates(s.lastDisk)
		s.lastDisk = current
		read = true
	} else {
		usage.DiskIOStatus, usage.DiskIOReason = statusFor(diskIOErr)
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
		return usage, lastErr
	}
	return usage, nil
}

func statusFor(err error) (MetricStatus, string) {
	if errors.Is(err, errUnsupported) {
		return MetricUnsupported, "unsupported"
	}
	return MetricError, "read_failed"
}

func OSVersion() string { return osVersion() }

func TotalMemory() uint64 {
	_, total, err := readMemory()
	if err != nil {
		return 0
	}
	return total
}

func TotalDisk(path string) uint64 {
	_, total, err := readDisk(path)
	if err != nil {
		return 0
	}
	return total
}

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

func parseNetDev(r io.Reader) (rx, tx uint64, err error) {
	scanner := bufio.NewScanner(r)
	seen := false
	for scanner.Scan() {
		name, counters, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(counters)
		if len(fields) < 9 {
			continue
		}
		received, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		transmitted, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
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

const sectorSize = 512

func parseDiskStats(r io.Reader, devices map[string]struct{}) (read, write uint64, err error) {
	scanner := bufio.NewScanner(r)
	seen := false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
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

func pseudoBlockDevice(name string) bool {
	return strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram")
}

func clampFraction(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
