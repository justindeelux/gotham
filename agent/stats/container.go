package stats

import (
	"math"
	"sync"
	"time"
)

// unlimitedMemory is Docker's sentinel range for a container with no memory
// limit (it reports a very large limit instead of omitting it).
const unlimitedMemory = uint64(1) << 62

// maxContainerSampleElapsed is the longest interval that still describes the
// same sampling run. A longer gap means the stream or agent was away, so the
// old counters are not a baseline for a value stamped at reconnect time.
const maxContainerSampleElapsed = 3 * 30 * time.Second

// ContainerCounters is one raw Docker stats reading. All counters are
// monotonic; a decrease means the container or engine counter restarted.
// The *Supported flags distinguish a measured zero from a metric the engine
// or container mode does not report at all.
type ContainerCounters struct {
	CPUTotal        uint64
	SystemCPU       uint64
	OnlineCPUs      uint32
	MemorySupported bool
	MemoryUsage     uint64
	MemoryCache     uint64
	MemoryLimit     uint64
	NetSupported    bool
	NetRx           uint64
	NetTx           uint64
	DiskSupported   bool
	DiskRead        uint64
	DiskWrite       uint64
}

// ContainerSample is the derived numeric evidence for one container.
// CPU and rates are nil when no valid delta exists (first sample, counter
// reset or nonpositive interval); they are never zero in that case.
type ContainerSample struct {
	CPUCores           *float64
	MemWorkingSetBytes *uint64
	MemLimitBytes      *uint64
	MemUnlimited       bool
	NetRxBps           *float64
	NetTxBps           *float64
	DiskReadBps        *float64
	DiskWriteBps       *float64
}

type containerCountersAt struct {
	ContainerCounters
	at time.Time
}

// ContainerSampler keeps each container's previous counters, keyed by the
// container ID, so one container's reset never disturbs another's rates.
type ContainerSampler struct {
	mu   sync.Mutex
	last map[string]containerCountersAt
}

func NewContainerSampler() *ContainerSampler {
	return &ContainerSampler{last: make(map[string]containerCountersAt)}
}

func (s *ContainerSampler) Observe(id string, counters ContainerCounters, at time.Time) ContainerSample {
	sample := ContainerSample{}
	if counters.MemorySupported {
		workingSet := workingSet(counters.MemoryUsage, counters.MemoryCache)
		sample.MemWorkingSetBytes = &workingSet
		limit := counters.MemoryLimit
		if limit == 0 || limit >= unlimitedMemory {
			sample.MemUnlimited = true
		} else {
			sample.MemLimitBytes = &limit
		}
	}

	s.mu.Lock()
	previous, ok := s.last[id]
	s.last[id] = containerCountersAt{ContainerCounters: counters, at: at}
	s.mu.Unlock()
	if !ok {
		return sample
	}
	elapsed := at.Sub(previous.at)
	if elapsed <= 0 || elapsed > maxContainerSampleElapsed {
		return sample
	}

	if counters.SystemCPU > previous.SystemCPU && counters.CPUTotal >= previous.CPUTotal && counters.OnlineCPUs > 0 {
		cores := float64(counters.CPUTotal-previous.CPUTotal) /
			float64(counters.SystemCPU-previous.SystemCPU) *
			float64(counters.OnlineCPUs)
		if cores >= 0 && !math.IsInf(cores, 0) && !math.IsNaN(cores) {
			sample.CPUCores = &cores
		}
	}
	if counters.NetSupported && previous.NetSupported {
		sample.NetRxBps = counterRate(counters.NetRx, previous.NetRx, elapsed)
		sample.NetTxBps = counterRate(counters.NetTx, previous.NetTx, elapsed)
	}
	if counters.DiskSupported && previous.DiskSupported {
		sample.DiskReadBps = counterRate(counters.DiskRead, previous.DiskRead, elapsed)
		sample.DiskWriteBps = counterRate(counters.DiskWrite, previous.DiskWrite, elapsed)
	}
	return sample
}

func (s *ContainerSampler) Retain(ids map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.last {
		if _, ok := ids[id]; !ok {
			delete(s.last, id)
		}
	}
}

func workingSet(usage, cache uint64) uint64 {
	if cache >= usage {
		return 0
	}
	return usage - cache
}

func counterRate(current, previous uint64, elapsed time.Duration) *float64 {
	if elapsed <= 0 || current < previous {
		return nil
	}
	rate := float64(current-previous) / elapsed.Seconds()
	if math.IsInf(rate, 0) || math.IsNaN(rate) {
		return nil
	}
	return &rate
}
