package domainwatch

import (
	"time"
)

// sample is the minimal persisted result the availability arithmetic needs.
type sample struct {
	At            time.Time
	IntervalStart time.Time
	Outcome       string
	Latency       time.Duration
	ValidSeconds  int64
}

func buildWindows(now time.Time, rows []sample) []Window {
	return []Window{
		buildWindow("24h", now.Add(-window24h), now, 10*time.Minute, rows),
		buildWindow("7d", now.Add(-window7d), now, time.Hour, rows),
	}
}

// buildWindow clips each result's valid interval to the window and leaves
// unknown gaps out of both the numerator and the denominator. A bucket with
// no completed probe stays unknown and never becomes a zero sample.
func buildWindow(key string, from, to time.Time, bucket time.Duration, rows []sample) Window {
	window := Window{
		Key:          key,
		RangeSeconds: int64(to.Sub(from) / time.Second),
		Points:       make([]Point, 0),
	}
	if window.RangeSeconds <= 0 {
		window.Insufficient = true
		return window
	}
	buckets := int(to.Sub(from)/bucket) + 1
	if buckets > maxPoints {
		buckets = maxPoints
	}
	if buckets < 1 {
		buckets = 1
	}
	type accumulator struct {
		healthy int64
		failed  int64
	}
	acc := make([]accumulator, buckets)
	latencyTotal := int64(0)
	latencyCount := 0
	keyStart := make([]time.Time, buckets)
	for i := range keyStart {
		keyStart[i] = from.Add(time.Duration(i) * bucket)
	}

	for _, row := range rows {
		if row.At.After(to) || row.ValidSeconds <= 0 {
			continue
		}
		start := row.IntervalStart
		if start.Before(from) {
			start = from
		}
		if !row.At.After(start) || !row.At.After(from) {
			continue
		}
		valid := int64(row.At.Sub(start) / time.Second)
		if valid > row.ValidSeconds {
			valid = row.ValidSeconds
		}
		if valid <= 0 {
			continue
		}
		window.ObservedSeconds += valid
		switch row.Outcome {
		case OutcomeHealthy:
			window.HealthySeconds += valid
		default:
			window.FailedSeconds += valid
		}
		window.Samples++
		if row.Outcome == OutcomeHealthy && row.Latency > 0 {
			latencyTotal += int64(row.Latency / time.Millisecond)
			latencyCount++
		}
		index := int(row.At.Sub(from) / bucket)
		if index < 0 {
			index = 0
		}
		if index >= buckets {
			index = buckets - 1
		}
		switch row.Outcome {
		case OutcomeHealthy:
			acc[index].healthy += valid
		default:
			acc[index].failed += valid
		}
	}

	if window.ObservedSeconds > window.RangeSeconds {
		window.ObservedSeconds = window.RangeSeconds
	}
	window.Coverage = float64(window.ObservedSeconds) / float64(window.RangeSeconds)
	if window.Coverage > 1 {
		window.Coverage = 1
	}
	if latencyCount > 0 {
		avg := int(latencyTotal / int64(latencyCount))
		window.AverageLatencyMS = &avg
	}
	window.Insufficient = window.Coverage < CoverageThreshold
	if !window.Insufficient && window.ObservedSeconds > 0 {
		availability := float64(window.HealthySeconds) / float64(window.ObservedSeconds)
		window.Availability = &availability
	}

	for i := range acc {
		point := Point{At: keyStart[i], Outcome: OutcomeUnknown}
		covered := acc[i].healthy + acc[i].failed
		point.CoveredSeconds = covered
		if covered > 0 {
			if acc[i].healthy >= acc[i].failed {
				point.Outcome = OutcomeHealthy
			} else {
				point.Outcome = OutcomeFailed
			}
		}
		window.Points = append(window.Points, point)
	}
	return window
}
