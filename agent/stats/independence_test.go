package stats

import (
	"testing"
	"time"
)

// TestSamplersKeepIndependentDeltas proves two samplers over the same fake
// counters cannot disturb each other: each rate is measured from that
// sampler's own previous sample, never from the other consumer's sample.
func TestSamplersKeepIndependentDeltas(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var netA, netB uint64
	reads := func(net *uint64) samplerReads {
		return samplerReads{
			readCPU:     func() (uint64, uint64, error) { return 0, 1, nil },
			readMemory:  func() (uint64, uint64, error) { return 1, 2, nil },
			readDisk:    func(string) (uint64, uint64, error) { return 1, 2, nil },
			readNetwork: func() (uint64, uint64, error) { return *net, 0, nil },
			readDiskIO:  func() (uint64, uint64, error) { return 0, 0, nil },
		}
	}
	a := newSampler(reads(&netA), func() time.Time { return now })
	b := newSampler(reads(&netB), func() time.Time { return now })

	if _, err := a.Sample(); err != nil {
		t.Fatalf("a first sample: %v", err)
	}
	now = now.Add(10 * time.Second)
	netA = 1000
	sampleA, err := a.Sample()
	if err != nil {
		t.Fatalf("a second sample: %v", err)
	}
	if sampleA.NetStatus != MetricOK || sampleA.NetRxBps != 100 {
		t.Fatalf("a rate = %v (%s); want 100 B/s over its own 10s", sampleA.NetRxBps, sampleA.NetStatus)
	}

	// B starts after A's second sample. Its first sample is warming and must
	// not consume A's baseline.
	firstB, err := b.Sample()
	if err != nil {
		t.Fatalf("b first sample: %v", err)
	}
	if firstB.NetStatus != MetricWarming {
		t.Fatalf("b first rate status = %s; want warming", firstB.NetStatus)
	}
	now = now.Add(5 * time.Second)
	netB = 500
	sampleB, err := b.Sample()
	if err != nil {
		t.Fatalf("b second sample: %v", err)
	}
	if sampleB.NetStatus != MetricOK || sampleB.NetRxBps != 100 {
		t.Fatalf("b rate = %v (%s); want 100 B/s over its own 5s", sampleB.NetRxBps, sampleB.NetStatus)
	}

	now = now.Add(10 * time.Second)
	netA = 2500
	sampleA, err = a.Sample()
	if err != nil {
		t.Fatalf("a third sample: %v", err)
	}
	if sampleA.NetStatus != MetricOK || sampleA.NetRxBps != 100 {
		t.Fatalf("a rate = %v (%s); want 100 B/s from A's own baseline", sampleA.NetRxBps, sampleA.NetStatus)
	}
}
