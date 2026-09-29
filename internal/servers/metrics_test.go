package servers

import (
	"errors"
	"testing"
	"time"
)

// TestParseMetricsQuery pins the range-query contract: the accepted steps, the
// default step, and the window caps that keep one request from materializing an
// unbounded series.
func TestParseMetricsQuery(t *testing.T) {
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		from    time.Time
		to      time.Time
		step    string
		want    time.Duration
		wantErr bool
	}{
		{"default step", from, from.Add(time.Hour), "", time.Minute, false},
		{"minute", from, from.Add(time.Hour), "1m", time.Minute, false},
		{"hour", from, from.Add(24 * time.Hour), "1h", time.Hour, false},
		{"day within retention", from, from.Add(30 * 24 * time.Hour), "1d", 24 * time.Hour, false},
		{"unknown step", from, from.Add(time.Hour), "5m", 0, true},
		{"missing from", time.Time{}, from, "1m", 0, true},
		{"missing to", from, time.Time{}, "1m", 0, true},
		{"inverted range", from, from.Add(-time.Minute), "1m", 0, true},
		{"empty range", from, from, "1m", 0, true},
		{"minute window too large", from, from.Add(25 * time.Hour), "1m", 0, true},
		{"hour window too large", from, from.Add(31 * 24 * time.Hour), "1h", 0, true},
		{"day window too large", from, from.Add(31 * 24 * time.Hour), "1d", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMetricsQuery(tt.from, tt.to, tt.step)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("ParseMetricsQuery = %v; want ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMetricsQuery: %v", err)
			}
			if got != tt.want {
				t.Errorf("bucket = %s; want %s", got, tt.want)
			}
		})
	}
}

// TestMetricsEnabledFlag pins the kill-switch semantics: only an explicit false
// disables metrics.
func TestMetricsEnabledFlag(t *testing.T) {
	for value, want := range map[string]bool{"": true, "true": true, "false": false, "FALSE": false, " false ": false} {
		t.Setenv(FeatureEnv, value)
		if got := MetricsEnabled(); got != want {
			t.Errorf("MetricsEnabled() with %q = %v; want %v", value, got, want)
		}
	}
}
