package stats

import (
	"errors"
	"testing"
	"time"
)

func TestSampleReportsMetricValidity(t *testing.T) {
	sampler := New()
	first, _ := sampler.Sample()

	if first.ObservedAt.IsZero() {
		t.Error("ObservedAt is zero; want the collection time")
	}
	if first.MemStatus != MetricOK || first.DiskStatus != MetricOK {
		t.Errorf("mem/disk status = %s/%s; want measured values", first.MemStatus, first.DiskStatus)
	}
	// A metric that could not be measured must not claim OK, and its value is
	// ignored by consumers. A measured zero stays OK.
	for name, status := range map[string]MetricStatus{
		"cpu":     first.CPUStatus,
		"net":     first.NetStatus,
		"disk io": first.DiskIOStatus,
	} {
		if status == "" {
			t.Errorf("%s status is empty; want explicit validity", name)
		}
	}
	if first.NetStatus == MetricOK {
		t.Error("the first counter sample must not be reported as a measured rate")
	}
	if first.CPUStatus == MetricOK {
		t.Error("the first CPU sample has no previous delta and must not be reported as measured")
	}
}

func TestStatusFor(t *testing.T) {
	tests := map[string]struct {
		err    error
		status MetricStatus
		reason string
	}{
		"unsupported": {err: errUnsupported, status: MetricUnsupported, reason: "unsupported"},
		"failed":      {err: errors.New("read failed"), status: MetricError, reason: "read_failed"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			status, reason := statusFor(tt.err)
			if status != tt.status || reason != tt.reason {
				t.Errorf("statusFor = %s/%s; want %s/%s", status, reason, tt.status, tt.reason)
			}
		})
	}
}

func TestCounterResetIsNotZero(t *testing.T) {
	previous := counterPair{in: 100, out: 200, at: time.Now(), valid: true}
	current := counterPair{in: 10, out: 20, at: time.Now().Add(time.Second), valid: true}
	if !current.reset(previous) {
		t.Error("counter reset was not detected")
	}
	if current.reset(counterPair{}) {
		t.Error("the first sample was treated as a reset")
	}
}
