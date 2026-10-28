package monitoring

import (
	"math"
	"testing"
	"time"

	"github.com/justindeelux/gotham/internal/incidents"
)

const gib = int64(1 << 30)

func hourlySeries(end time.Time, hours int, used func(int) int64) []ForecastBucket {
	out := make([]ForecastBucket, 0, hours)
	for i := 0; i < hours; i++ {
		out = append(out, ForecastBucket{
			At:        end.Add(-time.Duration(hours-1-i) * time.Hour),
			UsedBytes: used(i),
		})
	}
	return out
}

func TestForecastRisingTrendEstimatesHorizonAndFit(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	buckets := hourlySeries(end, 48, func(i int) int64 {
		return 50*gib + int64(i)*100*(1<<20)
	})
	forecast := EstimateForecast(buckets, 100*gib, end)
	if forecast.Status != ForecastEstimated {
		t.Fatalf("status = %s, want estimated", forecast.Status)
	}
	if forecast.Fit < 0.99 {
		t.Fatalf("fit = %.3f, want a clean linear fit", forecast.Fit)
	}
	wantSlope := float64(100 * (1 << 20))
	if math.Abs(forecast.SlopeBytesPerHour-wantSlope) > wantSlope*0.01 {
		t.Fatalf("slope = %.0f, want about %.0f", forecast.SlopeBytesPerHour, wantSlope)
	}
	if forecast.HorizonHours <= 0 || forecast.FillAt == nil {
		t.Fatalf("expected a positive horizon and fill time, got %+v", forecast)
	}
	if forecast.Samples != 48 || forecast.SpanHours != 47 {
		t.Fatalf("samples/span = %d/%d, want 48/47", forecast.Samples, forecast.SpanHours)
	}
}

func TestForecastShortHorizonIsCriticalBand(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	buckets := hourlySeries(end, 48, func(i int) int64 { return 50*gib + int64(i)*gib })
	forecast := EstimateForecast(buckets, 100*gib, end)
	if forecast.Status != ForecastEstimated {
		t.Fatalf("status = %s, want estimated", forecast.Status)
	}
	severity, unhealthy := forecastSeverity(horizonDuration(forecast.HorizonHours), "")
	if !unhealthy || severity != incidents.SeverityCritical {
		t.Fatalf("severity = %s/%v, want critical", severity, unhealthy)
	}
}

func TestForecastFlatAndFallingAreNonGrowing(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	flat := EstimateForecast(hourlySeries(end, 48, func(int) int64 { return 50 * gib }), 100*gib, end)
	if flat.Status != ForecastNonGrowing {
		t.Fatalf("flat status = %s, want non_growing", flat.Status)
	}
	falling := EstimateForecast(hourlySeries(end, 48, func(i int) int64 {
		return 80*gib - int64(i)*100*(1<<20)
	}), 100*gib, end)
	if falling.Status != ForecastNonGrowing {
		t.Fatalf("falling status = %s, want non_growing", falling.Status)
	}
}

func TestForecastNoisySeriesReportsLowConfidence(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	buckets := hourlySeries(end, 48, func(i int) int64 {
		return 300*gib + int64(i)*gib + int64(i%3)*60*gib
	})
	forecast := EstimateForecast(buckets, 2000*gib, end)
	if forecast.Status != ForecastLowConfidence {
		t.Fatalf("status = %s (fit %.2f), want low_confidence", forecast.Status, forecast.Fit)
	}
	if forecast.Fit >= forecastMinFit {
		t.Fatalf("fit = %.2f, want below %.2f", forecast.Fit, forecastMinFit)
	}
}

func TestForecastGappedAndShortSpanAreInsufficient(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	var gapped []ForecastBucket
	for i := 0; i < 48; i++ {
		if i%3 == 0 && i != 47 {
			continue
		}
		gapped = append(gapped, ForecastBucket{
			At:        end.Add(-time.Duration(47-i) * time.Hour),
			UsedBytes: 50*gib + int64(i)*100*(1<<20),
		})
	}
	if forecast := EstimateForecast(gapped, 100*gib, end); forecast.Status != ForecastInsufficient {
		t.Fatalf("gapped status = %s, want insufficient", forecast.Status)
	}
	short := hourlySeries(end, 12, func(i int) int64 { return 50*gib + int64(i)*100*(1<<20) })
	if forecast := EstimateForecast(short, 100*gib, end); forecast.Status != ForecastInsufficient {
		t.Fatalf("short status = %s, want insufficient", forecast.Status)
	}
}

func TestForecastStaleHistoryIsNotAnEstimate(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	buckets := hourlySeries(end, 48, func(i int) int64 { return 50*gib + int64(i)*gib })
	forecast := EstimateForecast(buckets, 100*gib, end.Add(3*time.Hour))
	if forecast.Status != ForecastStale {
		t.Fatalf("status = %s, want stale", forecast.Status)
	}
}

func TestForecastIgnoresExtraSamplesInsideAnHour(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 30, 0, 0, time.UTC)
	clean := hourlySeries(end, 48, func(i int) int64 { return 50*gib + int64(i)*100*(1<<20) })
	uneven := make([]ForecastBucket, 0, len(clean)*2)
	for _, bucket := range clean {
		// A noisy sample earlier in the same hour must not add a point or
		// change the slope; the hour keeps its last observation.
		uneven = append(uneven,
			ForecastBucket{At: bucket.At.Add(-20 * time.Minute), UsedBytes: bucket.UsedBytes - 20*gib},
			ForecastBucket{At: bucket.At, UsedBytes: bucket.UsedBytes},
		)
	}
	want := EstimateForecast(clean, 100*gib, end)
	got := EstimateForecast(uneven, 100*gib, end)
	if got.Status != want.Status || got.Samples != want.Samples ||
		math.Abs(got.SlopeBytesPerHour-want.SlopeBytesPerHour) > 1 {
		t.Fatalf("uneven cadence changed the estimate: %+v vs %+v", got, want)
	}
}

func TestForecastCleanupStartsANewBaseline(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	cleanupAt := 20
	buckets := hourlySeries(end, 48, func(i int) int64 {
		if i < cleanupAt {
			return 80*gib + int64(i)*100*(1<<20)
		}
		// Free 40% of a 100 GiB disk in one hourly bucket.
		return 40*gib + int64(i-cleanupAt)*100*(1<<20)
	})
	forecast := EstimateForecast(buckets, 100*gib, end)
	if forecast.Status != ForecastEstimated {
		t.Fatalf("status = %s, want estimated from the new baseline", forecast.Status)
	}
	wantFrom := end.Add(-time.Duration(47-cleanupAt) * time.Hour)
	if forecast.WindowFrom == nil || !forecast.WindowFrom.Equal(wantFrom) {
		t.Fatalf("window from = %v, want %v", forecast.WindowFrom, wantFrom)
	}
	if forecast.Samples != 28 {
		t.Fatalf("samples = %d, want 28 after the cleanup", forecast.Samples)
	}
}

func TestForecastCleanupThatLeavesTooLittleHistoryIsInsufficient(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	buckets := hourlySeries(end, 48, func(i int) int64 {
		if i < 30 {
			return 80*gib + int64(i)*100*(1<<20)
		}
		return 40*gib + int64(i-30)*100*(1<<20)
	})
	if forecast := EstimateForecast(buckets, 100*gib, end); forecast.Status != ForecastInsufficient {
		t.Fatalf("status = %s, want insufficient after the reset", forecast.Status)
	}
}

func TestForecastSeverityHysteresis(t *testing.T) {
	cases := []struct {
		name    string
		horizon time.Duration
		current incidents.Severity
		want    incidents.Severity
		unhappy bool
	}{
		{"critical within a day", 10 * time.Hour, "", incidents.SeverityCritical, true},
		{"warning within a week", 4 * 24 * time.Hour, "", incidents.SeverityWarning, true},
		{"critical holds above a day", 40 * time.Hour, incidents.SeverityCritical, incidents.SeverityCritical, true},
		{"critical downgrades after two days", 60 * time.Hour, incidents.SeverityCritical, incidents.SeverityWarning, true},
		{"warning holds until two weeks", 10 * 24 * time.Hour, incidents.SeverityWarning, incidents.SeverityWarning, true},
		{"clear above two weeks", 15 * 24 * time.Hour, incidents.SeverityCritical, "", false},
		{"no trigger between a week and two weeks", 10 * 24 * time.Hour, "", "", false},
	}
	for _, test := range cases {
		severity, unhealthy := forecastSeverity(test.horizon, test.current)
		if severity != test.want || unhealthy != test.unhappy {
			t.Fatalf("%s: got %s/%v, want %s/%v", test.name, severity, unhealthy, test.want, test.unhappy)
		}
	}
}

func TestForecastIgnoresMissingHoursInsteadOfInventingRecovery(t *testing.T) {
	end := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	clean := hourlySeries(end, 48, func(i int) int64 { return 50*gib + int64(i)*gib })
	// Drop every third hour: the gap is preserved, the remaining trend still
	// fits, and no dropped hour becomes a zero or a healthy reading.
	gapped := make([]ForecastBucket, 0, len(clean))
	for i, bucket := range clean {
		if i%4 != 0 || i == len(clean)-1 {
			gapped = append(gapped, bucket)
		}
	}
	forecast := EstimateForecast(gapped, 200*gib, end)
	if forecast.Status != ForecastEstimated {
		t.Fatalf("status = %s, want estimated", forecast.Status)
	}
	if forecast.Samples != 36 || forecast.HorizonHours <= 0 {
		t.Fatalf("samples/horizon = %d/%.1f, want 36/positive", forecast.Samples, forecast.HorizonHours)
	}
}
