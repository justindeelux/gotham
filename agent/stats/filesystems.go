package stats

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Filesystem purposes reported with each filesystem. They are stable strings
// so consumers can group capacity evidence without guessing from the path.
const (
	PurposeRoot           = "root"
	PurposeDockerData     = "docker_data"
	PurposeManagedStorage = "managed_storage"
	PurposeBackup         = "backup"
	defaultTopologyMaxAge = time.Minute
)

// FilesystemCandidate is one path the agent should report capacity for.
type FilesystemCandidate struct {
	Path    string
	Purpose string
}

// Filesystem is one filesystem's capacity evidence. A zero total with
// MetricOK is a real measured zero; every other status means unknown.
type Filesystem struct {
	Path           string
	Purpose        string
	Status         MetricStatus
	Reason         string
	TotalBytes     uint64
	AvailableBytes uint64
	InodeStatus    MetricStatus
	InodeReason    string
	TotalInodes    uint64
	FreeInodes     uint64
}

type capacity struct {
	totalBytes     uint64
	availableBytes uint64
	totalInodes    uint64
	freeInodes     uint64
	kind           int64
}

type topologyEntry struct {
	path      string
	purpose   string
	device    uint64
	hasDevice bool
}

// FilesystemCollector discovers the candidate filesystems at most once per
// topology window and samples their capacity counters on every call. Paths on
// the same device are deduplicated; a candidate whose capacity read fails is
// reported as an error rather than as zero.
type FilesystemCollector struct {
	mu          sync.Mutex
	now         func() time.Time
	maxAge      time.Duration
	entries     []topologyEntry
	refreshedAt time.Time
	capacity    func(string) (capacity, error)
	device      func(string) (uint64, error)
}

func NewFilesystemCollector() *FilesystemCollector {
	return &FilesystemCollector{
		now:      time.Now,
		maxAge:   defaultTopologyMaxAge,
		capacity: statfsCapacity,
		device:   fileDevice,
	}
}

func (c *FilesystemCollector) Collect(candidates []FilesystemCandidate) []Filesystem {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.entries == nil || now.Sub(c.refreshedAt) >= c.maxAge {
		c.entries = c.resolve(candidates)
		c.refreshedAt = now
	}

	out := make([]Filesystem, 0, len(c.entries))
	for _, entry := range c.entries {
		observation := Filesystem{Path: entry.path, Purpose: entry.purpose}
		value, err := c.capacity(entry.path)
		if err != nil {
			observation.Status, observation.Reason = MetricError, "read_failed"
			observation.InodeStatus, observation.InodeReason = MetricError, "read_failed"
			// A path that stopped answering is a known topology change: re-
			// discover on the next call instead of waiting for the window.
			c.refreshedAt = time.Time{}
			out = append(out, observation)
			continue
		}
		observation.Status = MetricOK
		observation.TotalBytes = value.totalBytes
		observation.AvailableBytes = value.availableBytes
		if observation.AvailableBytes > observation.TotalBytes {
			observation.AvailableBytes = observation.TotalBytes
		}
		if value.totalInodes == 0 {
			observation.InodeStatus, observation.InodeReason = MetricUnsupported, "unsupported"
		} else {
			observation.InodeStatus = MetricOK
			observation.TotalInodes = value.totalInodes
			observation.FreeInodes = value.freeInodes
			if observation.FreeInodes > observation.TotalInodes {
				observation.FreeInodes = observation.TotalInodes
			}
		}
		out = append(out, observation)
	}
	return out
}

func (c *FilesystemCollector) resolve(candidates []FilesystemCandidate) []topologyEntry {
	entries := make([]topologyEntry, 0, len(candidates))
	seenPath := make(map[string]bool, len(candidates))
	seenDevice := make(map[uint64]bool, len(candidates))
	for _, candidate := range candidates {
		path := filepath.Clean(strings.TrimSpace(candidate.Path))
		if path == "" || path == "." || seenPath[path] {
			continue
		}
		seenPath[path] = true
		value, err := c.capacity(path)
		if err != nil {
			entries = append(entries, topologyEntry{path: path, purpose: candidate.Purpose})
			continue
		}
		if pseudoFilesystem(value.kind) {
			continue
		}
		device, err := c.device(path)
		if err != nil {
			entries = append(entries, topologyEntry{path: path, purpose: candidate.Purpose})
			continue
		}
		if seenDevice[device] {
			continue
		}
		seenDevice[device] = true
		entries = append(entries, topologyEntry{path: path, purpose: candidate.Purpose, device: device, hasDevice: true})
	}
	return entries
}

func statfsCapacity(path string) (capacity, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return capacity{}, err
	}
	blockSize := uint64(stat.Bsize)
	return capacity{
		totalBytes:     stat.Blocks * blockSize,
		availableBytes: stat.Bavail * blockSize,
		totalInodes:    stat.Files,
		freeInodes:     stat.Ffree,
		kind:           int64(stat.Type),
	}, nil
}

func fileDevice(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, syscall.EINVAL
	}
	return uint64(stat.Dev), nil
}
