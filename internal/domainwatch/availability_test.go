package domainwatch

import (
	"testing"
	"time"
)

func TestWindowExcludesUnknownGaps(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	rows := []sample{
		{At: now.Add(-2 * time.Hour), IntervalStart: now.Add(-3 * time.Hour), Outcome: OutcomeHealthy, ValidSeconds: 3600, Latency: 40 * time.Millisecond},
		{At: now.Add(-time.Hour), IntervalStart: now.Add(-2 * time.Hour), Outcome: OutcomeFailed, ValidSeconds: 3600},
	}
	window := buildWindow("24h", now.Add(-24*time.Hour), now, 10*time.Minute, rows)
	if window.ObservedSeconds != 7200 {
		t.Fatalf("observed = %d", window.ObservedSeconds)
	}
	if window.Coverage > 0.1 {
		t.Fatalf("coverage = %v", window.Coverage)
	}
	if !window.Insufficient || window.Availability != nil {
		t.Fatalf("insufficient=%v availability=%v", window.Insufficient, window.Availability)
	}
}

func TestWindowAvailabilityAndLatencyIgnoreFailures(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	rows := make([]sample, 0, 1440)
	for i := 0; i < 1440; i++ {
		at := now.Add(-24*time.Hour + time.Duration(i+1)*time.Minute)
		row := sample{At: at, IntervalStart: at.Add(-time.Minute), Outcome: OutcomeHealthy, ValidSeconds: 60, Latency: 20 * time.Millisecond}
		if i == 1000 {
			row.Outcome = OutcomeFailed
			row.Latency = 5 * time.Second
		}
		rows = append(rows, row)
	}
	window := buildWindow("24h", now.Add(-24*time.Hour), now, 10*time.Minute, rows)
	if window.Insufficient {
		t.Fatalf("coverage = %v", window.Coverage)
	}
	if window.Availability == nil {
		t.Fatal("availability missing")
	}
	want := 1439.0 / 1440.0
	if diff := *window.Availability - want; diff < -0.001 || diff > 0.001 {
		t.Fatalf("availability = %v, want %v", *window.Availability, want)
	}
	if window.AverageLatencyMS == nil || *window.AverageLatencyMS != 20 {
		t.Fatalf("latency = %v", window.AverageLatencyMS)
	}
}

func TestWindowPointsAreBoundedAndUnknownBucketsStayUnknown(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	rows := []sample{{
		At:            now.Add(-time.Minute),
		IntervalStart: now.Add(-2 * time.Minute),
		Outcome:       OutcomeHealthy,
		ValidSeconds:  60,
	}}
	window := buildWindow("7d", now.Add(-7*24*time.Hour), now, time.Hour, rows)
	if len(window.Points) > maxPoints {
		t.Fatalf("points = %d", len(window.Points))
	}
	unknown := 0
	for _, point := range window.Points {
		if point.Outcome == OutcomeUnknown {
			unknown++
		}
	}
	if unknown != len(window.Points)-1 {
		t.Fatalf("unknown points = %d of %d", unknown, len(window.Points))
	}
}

func TestFirstProbeAfterRestartCountsNoHealthyTime(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	rows := []sample{{
		At:            now,
		IntervalStart: now, // no previous completed probe
		Outcome:       OutcomeHealthy,
		ValidSeconds:  0,
	}}
	window := buildWindow("24h", now.Add(-24*time.Hour), now, 10*time.Minute, rows)
	if window.ObservedSeconds != 0 || window.Availability != nil {
		t.Fatalf("observed=%d availability=%v", window.ObservedSeconds, window.Availability)
	}
}
