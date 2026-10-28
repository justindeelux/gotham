package stats

import (
	"testing"
	"time"
)

func almostEqual(a, b float64) bool {
	diff := a - b
	return diff < 1e-9 && diff > -1e-9
}

func TestContainerFirstSampleIsAWarmingGap(t *testing.T) {
	sampler := NewContainerSampler()
	at := time.Unix(1000, 0)
	sample := sampler.Observe("abc", ContainerCounters{
		CPUTotal:    100,
		SystemCPU:   1000,
		OnlineCPUs:  4,
		MemoryUsage: 1000,
		MemoryCache: 200,
		MemoryLimit: 4096,
		NetRx:       500,
		NetTx:       600,
		DiskRead:    700,
		DiskWrite:   800,
	}, at)

	if sample.CPUCores != nil {
		t.Errorf("first sample CPU = %v; want a warming gap", *sample.CPUCores)
	}
	if sample.NetRxBps != nil || sample.NetTxBps != nil || sample.DiskReadBps != nil || sample.DiskWriteBps != nil {
		t.Error("first sample rates must be gaps, not zero")
	}
	if sample.MemWorkingSetBytes != 800 {
		t.Errorf("working set = %d; want usage minus cache 800", sample.MemWorkingSetBytes)
	}
	if sample.MemLimitBytes == nil || *sample.MemLimitBytes != 4096 {
		t.Errorf("limit = %v; want 4096", sample.MemLimitBytes)
	}
	if sample.MemUnlimited {
		t.Error("limited container marked unlimited")
	}
}

func TestContainerSecondSampleCalculatesCoresAndRates(t *testing.T) {
	sampler := NewContainerSampler()
	first := time.Unix(1000, 0)
	second := first.Add(30 * time.Second)
	sampler.Observe("abc", ContainerCounters{
		CPUTotal: 100, SystemCPU: 10_000, OnlineCPUs: 4,
		NetRx: 1000, NetTx: 2000, DiskRead: 3000, DiskWrite: 4000,
	}, first)
	sample := sampler.Observe("abc", ContainerCounters{
		CPUTotal: 400, SystemCPU: 40_000, OnlineCPUs: 4,
		NetRx: 4000, NetTx: 5000, DiskRead: 6000, DiskWrite: 7000,
	}, second)

	// 300 CPU ticks of a 30000-tick system delta on 4 CPUs = 0.04 cores.
	if sample.CPUCores == nil || !almostEqual(*sample.CPUCores, 0.04) {
		t.Fatalf("CPU cores = %v; want 0.04", sample.CPUCores)
	}
	if sample.NetRxBps == nil || !almostEqual(*sample.NetRxBps, 100) {
		t.Errorf("rx = %v; want 100 B/s", sample.NetRxBps)
	}
	if sample.NetTxBps == nil || !almostEqual(*sample.NetTxBps, 100) {
		t.Errorf("tx = %v; want 100 B/s", sample.NetTxBps)
	}
	if sample.DiskReadBps == nil || !almostEqual(*sample.DiskReadBps, 100) {
		t.Errorf("read = %v; want 100 B/s", sample.DiskReadBps)
	}
	if sample.DiskWriteBps == nil || !almostEqual(*sample.DiskWriteBps, 100) {
		t.Errorf("write = %v; want 100 B/s", sample.DiskWriteBps)
	}
}

func TestContainerCounterResetIsAGapThenRecovers(t *testing.T) {
	sampler := NewContainerSampler()
	first := time.Unix(1000, 0)
	second := first.Add(30 * time.Second)
	third := second.Add(30 * time.Second)
	sampler.Observe("abc", ContainerCounters{CPUTotal: 100, SystemCPU: 1000, OnlineCPUs: 2, NetRx: 1000}, first)
	reset := sampler.Observe("abc", ContainerCounters{CPUTotal: 10, SystemCPU: 2000, OnlineCPUs: 2, NetRx: 10}, second)
	if reset.CPUCores != nil {
		t.Errorf("CPU after total counter reset = %v; want a gap", *reset.CPUCores)
	}
	if reset.NetRxBps != nil {
		t.Errorf("rx after counter reset = %v; want a gap", *reset.NetRxBps)
	}
	recovered := sampler.Observe("abc", ContainerCounters{CPUTotal: 20, SystemCPU: 3000, OnlineCPUs: 2, NetRx: 40}, third)
	if recovered.NetRxBps == nil || !almostEqual(*recovered.NetRxBps, 1) {
		t.Errorf("rx after reset recovery = %v; want 1 B/s from the new baseline", recovered.NetRxBps)
	}
}

func TestContainerNonPositiveIntervalIsAGap(t *testing.T) {
	sampler := NewContainerSampler()
	at := time.Unix(1000, 0)
	sampler.Observe("abc", ContainerCounters{NetRx: 100}, at)
	sample := sampler.Observe("abc", ContainerCounters{NetRx: 200}, at)
	if sample.NetRxBps != nil {
		t.Errorf("rx with zero elapsed = %v; want a gap", *sample.NetRxBps)
	}
}

func TestContainerUnlimitedMemoryHasNoLimit(t *testing.T) {
	sampler := NewContainerSampler()
	sample := sampler.Observe("abc", ContainerCounters{
		MemoryUsage: 2048,
		MemoryLimit: unlimitedMemory,
	}, time.Unix(1000, 0))
	if !sample.MemUnlimited {
		t.Error("sentinel limit must be reported as unlimited")
	}
	if sample.MemLimitBytes != nil {
		t.Errorf("unlimited container limit = %v; want unknown", *sample.MemLimitBytes)
	}
	if sample.MemWorkingSetBytes != 2048 {
		t.Errorf("working set = %d; want 2048", sample.MemWorkingSetBytes)
	}
}

func TestContainerMissingCPUCountersIsAGap(t *testing.T) {
	sampler := NewContainerSampler()
	first := time.Unix(1000, 0)
	sampler.Observe("abc", ContainerCounters{CPUTotal: 100, SystemCPU: 1000}, first)
	sample := sampler.Observe("abc", ContainerCounters{CPUTotal: 200, SystemCPU: 2000}, first.Add(10*time.Second))
	if sample.CPUCores != nil {
		t.Errorf("CPU without online CPUs = %v; want a gap", *sample.CPUCores)
	}
}

func TestContainerSamplerRetainDropsGoneContainers(t *testing.T) {
	sampler := NewContainerSampler()
	sampler.Observe("a", ContainerCounters{}, time.Unix(1000, 0))
	sampler.Observe("b", ContainerCounters{}, time.Unix(1000, 0))
	sampler.Retain(map[string]struct{}{"a": {}})
	if _, ok := sampler.last["b"]; ok {
		t.Error("removed container baseline was retained")
	}
	if _, ok := sampler.last["a"]; !ok {
		t.Error("present container baseline was dropped")
	}
}
