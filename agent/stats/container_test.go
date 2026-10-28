package stats

import (
	"testing"
	"time"
)

func almostEqual(a, b float64) bool {
	diff := a - b
	return diff < 1e-9 && diff > -1e-9
}

func supportedCounters() ContainerCounters {
	return ContainerCounters{
		MemorySupported: true,
		NetSupported:    true,
		DiskSupported:   true,
	}
}

func TestContainerFirstSampleIsAWarmingGap(t *testing.T) {
	sampler := NewContainerSampler()
	at := time.Unix(1000, 0)
	counters := supportedCounters()
	counters.CPUTotal = 100
	counters.SystemCPU = 1000
	counters.OnlineCPUs = 4
	counters.MemoryUsage = 1000
	counters.MemoryCache = 200
	counters.MemoryLimit = 4096
	counters.NetRx = 500
	counters.NetTx = 600
	counters.DiskRead = 700
	counters.DiskWrite = 800
	sample := sampler.Observe("abc", counters, at)

	if sample.CPUCores != nil {
		t.Errorf("first sample CPU = %v; want a warming gap", *sample.CPUCores)
	}
	if sample.NetRxBps != nil || sample.NetTxBps != nil || sample.DiskReadBps != nil || sample.DiskWriteBps != nil {
		t.Error("first sample rates must be gaps, not zero")
	}
	if sample.MemWorkingSetBytes == nil || *sample.MemWorkingSetBytes != 800 {
		t.Errorf("working set = %v; want usage minus cache 800", sample.MemWorkingSetBytes)
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
	previous := supportedCounters()
	previous.CPUTotal = 100
	previous.SystemCPU = 10_000
	previous.OnlineCPUs = 4
	previous.NetRx = 1000
	previous.NetTx = 2000
	previous.DiskRead = 3000
	previous.DiskWrite = 4000
	sampler.Observe("abc", previous, first)
	current := supportedCounters()
	current.CPUTotal = 400
	current.SystemCPU = 40_000
	current.OnlineCPUs = 4
	current.NetRx = 4000
	current.NetTx = 5000
	current.DiskRead = 6000
	current.DiskWrite = 7000
	sample := sampler.Observe("abc", current, second)

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
	initial := supportedCounters()
	initial.CPUTotal = 100
	initial.SystemCPU = 1000
	initial.OnlineCPUs = 2
	initial.NetRx = 1000
	sampler.Observe("abc", initial, first)
	resetCounters := supportedCounters()
	resetCounters.CPUTotal = 10
	resetCounters.SystemCPU = 2000
	resetCounters.OnlineCPUs = 2
	resetCounters.NetRx = 10
	reset := sampler.Observe("abc", resetCounters, second)
	if reset.CPUCores != nil {
		t.Errorf("CPU after total counter reset = %v; want a gap", *reset.CPUCores)
	}
	if reset.NetRxBps != nil {
		t.Errorf("rx after counter reset = %v; want a gap", *reset.NetRxBps)
	}
	recoveredCounters := supportedCounters()
	recoveredCounters.CPUTotal = 20
	recoveredCounters.SystemCPU = 3000
	recoveredCounters.OnlineCPUs = 2
	recoveredCounters.NetRx = 40
	recovered := sampler.Observe("abc", recoveredCounters, third)
	if recovered.NetRxBps == nil || !almostEqual(*recovered.NetRxBps, 1) {
		t.Errorf("rx after reset recovery = %v; want 1 B/s from the new baseline", recovered.NetRxBps)
	}
}

func TestContainerNonPositiveIntervalIsAGap(t *testing.T) {
	sampler := NewContainerSampler()
	at := time.Unix(1000, 0)
	first := supportedCounters()
	first.NetRx = 100
	sampler.Observe("abc", first, at)
	second := supportedCounters()
	second.NetRx = 200
	sample := sampler.Observe("abc", second, at)
	if sample.NetRxBps != nil {
		t.Errorf("rx with zero elapsed = %v; want a gap", *sample.NetRxBps)
	}
}

func TestContainerLongElapsedResetsTheBaseline(t *testing.T) {
	sampler := NewContainerSampler()
	first := time.Unix(1000, 0)
	firstCounters := supportedCounters()
	firstCounters.CPUTotal = 100
	firstCounters.SystemCPU = 1000
	firstCounters.OnlineCPUs = 2
	firstCounters.NetRx = 1000
	sampler.Observe("abc", firstCounters, first)

	afterOutage := first.Add(3 * time.Minute)
	secondCounters := supportedCounters()
	secondCounters.CPUTotal = 10_000
	secondCounters.SystemCPU = 100_000
	secondCounters.OnlineCPUs = 2
	secondCounters.NetRx = 100_000
	sample := sampler.Observe("abc", secondCounters, afterOutage)
	if sample.CPUCores != nil || sample.NetRxBps != nil {
		t.Errorf("rate after a long outage = %v/%v; want fresh-baseline gaps", sample.CPUCores, sample.NetRxBps)
	}

	nextCounters := supportedCounters()
	nextCounters.CPUTotal = 10_060
	nextCounters.SystemCPU = 100_300
	nextCounters.OnlineCPUs = 2
	nextCounters.NetRx = 100_300
	recovered := sampler.Observe("abc", nextCounters, afterOutage.Add(30*time.Second))
	if recovered.NetRxBps == nil || !almostEqual(*recovered.NetRxBps, 10) {
		t.Errorf("rx after the fresh baseline = %v; want 10 B/s", recovered.NetRxBps)
	}
}

func TestContainerUnsupportedCountersStayUnknown(t *testing.T) {
	sampler := NewContainerSampler()
	first := time.Unix(1000, 0)
	// A host/none network and an empty blkio list report no counters, and the
	// engine may omit memory_stats. None of these are a measured zero.
	firstCounters := ContainerCounters{CPUTotal: 100, SystemCPU: 1000, OnlineCPUs: 2}
	sampler.Observe("abc", firstCounters, first)
	sample := sampler.Observe("abc", ContainerCounters{CPUTotal: 200, SystemCPU: 2000, OnlineCPUs: 2}, first.Add(30*time.Second))
	if sample.MemWorkingSetBytes != nil {
		t.Errorf("unsupported memory = %v; want unknown", *sample.MemWorkingSetBytes)
	}
	if sample.MemUnlimited || sample.MemLimitBytes != nil {
		t.Errorf("unsupported memory limit = %v/%v; want unknown", sample.MemUnlimited, sample.MemLimitBytes)
	}
	if sample.NetRxBps != nil || sample.NetTxBps != nil {
		t.Errorf("unsupported network rates = %v/%v; want unknown", sample.NetRxBps, sample.NetTxBps)
	}
	if sample.DiskReadBps != nil || sample.DiskWriteBps != nil {
		t.Errorf("unsupported disk rates = %v/%v; want unknown", sample.DiskReadBps, sample.DiskWriteBps)
	}
}

func TestContainerSupportedZeroIsAMeasuredZero(t *testing.T) {
	sampler := NewContainerSampler()
	first := time.Unix(1000, 0)
	sampler.Observe("abc", supportedCounters(), first)
	sample := sampler.Observe("abc", supportedCounters(), first.Add(30*time.Second))
	if sample.MemWorkingSetBytes == nil || *sample.MemWorkingSetBytes != 0 {
		t.Errorf("measured zero memory = %v; want a known zero", sample.MemWorkingSetBytes)
	}
	if sample.NetRxBps == nil || !almostEqual(*sample.NetRxBps, 0) {
		t.Errorf("measured zero rx = %v; want a known zero", sample.NetRxBps)
	}
}

func TestContainerUnlimitedMemoryHasNoLimit(t *testing.T) {
	sampler := NewContainerSampler()
	counters := supportedCounters()
	counters.MemoryUsage = 2048
	counters.MemoryLimit = unlimitedMemory
	sample := sampler.Observe("abc", counters, time.Unix(1000, 0))
	if !sample.MemUnlimited {
		t.Error("sentinel limit must be reported as unlimited")
	}
	if sample.MemLimitBytes != nil {
		t.Errorf("unlimited container limit = %v; want unknown", *sample.MemLimitBytes)
	}
	if sample.MemWorkingSetBytes == nil || *sample.MemWorkingSetBytes != 2048 {
		t.Errorf("working set = %v; want 2048", sample.MemWorkingSetBytes)
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
