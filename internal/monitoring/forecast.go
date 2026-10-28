package monitoring

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Filesystem exhaustion forecast. The estimator is deliberately deterministic:
// one least-squares slope over hourly buckets, never an AI provider or an
// external monitoring service.
const (
	DetectorForecast = "filesystem_forecast"

	forecastWindow        = 7 * 24 * time.Hour
	forecastMinSpan       = 24 * time.Hour
	forecastMinSamples    = 12
	forecastMinCoverage   = 0.75
	forecastMaxAge        = 2 * time.Hour
	forecastMinFit        = 0.8
	forecastCleanupJump   = 0.10
	forecastWarning       = 7 * 24 * time.Hour
	forecastCritical      = 24 * time.Hour
	forecastDowngrade     = 48 * time.Hour
	forecastClear         = 14 * 24 * time.Hour
	forecastConfirm       = time.Hour
	forecastRecover       = time.Hour
	forecastMaxGap        = 2 * time.Hour
	forecastInterval      = time.Hour
	forecastSweepTimeout  = 2 * time.Minute
	forecastMaxHorizonHrs = 200 * 365 * 24
)

// ForecastStatus is the disclosed result of one estimation. Only estimated is
// a prediction; the others explain why Gotham will not extrapolate.
type ForecastStatus string

const (
	ForecastEstimated     ForecastStatus = "estimated"
	ForecastInsufficient  ForecastStatus = "insufficient"
	ForecastStale         ForecastStatus = "stale"
	ForecastLowConfidence ForecastStatus = "low_confidence"
	ForecastNonGrowing    ForecastStatus = "non_growing"
)

// ForecastBucket is one hourly filesystem usage observation. UsedBytes is the
// last used-byte value in that hour; a missing hour is a gap, never a zero.
type ForecastBucket struct {
	At        time.Time
	UsedBytes int64
}

// FilesystemForecast is the read model shared by server storage details and
// incident evidence. HorizonHours is only meaningful when Status is estimated.
type FilesystemForecast struct {
	Status            ForecastStatus `json:"status"`
	AvailableBytes    uint64         `json:"available_bytes"`
	TotalBytes        uint64         `json:"total_bytes"`
	WindowFrom        *time.Time     `json:"window_from,omitempty"`
	WindowTo          *time.Time     `json:"window_to,omitempty"`
	SpanHours         int            `json:"span_hours"`
	Samples           int            `json:"samples"`
	Coverage          float64        `json:"coverage"`
	SlopeBytesPerHour float64        `json:"slope_bytes_per_hour"`
	Fit               float64        `json:"fit"`
	HorizonHours      float64        `json:"horizon_hours,omitempty"`
	FillAt            *time.Time     `json:"fill_at,omitempty"`
}

type forecastPoint struct {
	x float64
	y float64
}

// EstimateForecast normalises used-byte observations into hourly points and
// fits one time-based trend. It returns insufficient/stale/low-confidence/
// non-growing instead of guessing when the evidence does not qualify.
func EstimateForecast(buckets []ForecastBucket, totalBytes int64, now time.Time) FilesystemForecast {
	now = now.UTC()
	out := FilesystemForecast{Status: ForecastInsufficient}
	if totalBytes <= 0 {
		return out
	}
	capacity := float64(totalBytes)
	out.TotalBytes = uint64(totalBytes)

	points := normaliseForecastBuckets(buckets, totalBytes, now)
	if len(points) == 0 {
		return out
	}
	latest := points[len(points)-1]
	out.AvailableBytes = availableBytes(totalBytes, latest.UsedBytes)

	// A cleanup is a free-space jump of at least 10% of capacity between two
	// observations. The new baseline starts at the bucket after the jump, so
	// the fit never runs through space that was freed.
	start := 0
	for i := 1; i < len(points); i++ {
		freeBefore := capacity - float64(points[i-1].UsedBytes)
		freeAfter := capacity - float64(points[i].UsedBytes)
		if freeAfter-freeBefore >= capacity*forecastCleanupJump {
			start = i
		}
	}
	points = points[start:]
	first := points[0].At
	last := points[len(points)-1].At

	span := last.Sub(first)
	spanHours := int(span / time.Hour)
	out.WindowFrom = timePtr(first)
	out.WindowTo = timePtr(last)
	out.SpanHours = spanHours
	out.Samples = len(points)
	hours := spanHours + 1
	if hours > 0 {
		out.Coverage = float64(len(points)) / float64(hours)
	}

	if now.Sub(last) > forecastMaxAge {
		out.Status = ForecastStale
		return out
	}
	if span < forecastMinSpan || len(points) < forecastMinSamples || out.Coverage < forecastMinCoverage {
		out.Status = ForecastInsufficient
		return out
	}

	fitPoints := make([]forecastPoint, 0, len(points))
	for _, point := range points {
		fitPoints = append(fitPoints, forecastPoint{
			x: point.At.Sub(first).Hours(),
			y: float64(point.UsedBytes),
		})
	}
	slope, fit := linearFit(fitPoints)
	out.SlopeBytesPerHour = slope
	out.Fit = fit

	mid := first.Add(span / 2)
	recent := make([]forecastPoint, 0, len(points))
	for _, point := range fitPoints {
		if first.Add(time.Duration(point.x * float64(time.Hour))).Before(mid) {
			continue
		}
		recent = append(recent, point)
	}
	if len(recent) < 2 {
		out.Status = ForecastInsufficient
		return out
	}
	recentSlope, _ := linearFit(recent)

	// Growth has to be visible over the whole window and in its recent half.
	// A cleanup that landed inside a gap therefore cannot look like recovery.
	if slope <= 0 || recentSlope <= 0 {
		out.Status = ForecastNonGrowing
		return out
	}
	if fit < forecastMinFit {
		out.Status = ForecastLowConfidence
		return out
	}
	if out.AvailableBytes == 0 {
		out.Status = ForecastEstimated
		out.HorizonHours = 0
		at := last
		out.FillAt = &at
		return out
	}

	horizon := float64(out.AvailableBytes) / slope
	if math.IsNaN(horizon) || math.IsInf(horizon, 0) || horizon <= 0 || horizon > forecastMaxHorizonHrs {
		out.Status = ForecastNonGrowing
		return out
	}
	out.Status = ForecastEstimated
	out.HorizonHours = horizon
	at := last.Add(time.Duration(horizon * float64(time.Hour)))
	out.FillAt = &at
	return out
}

// normaliseForecastBuckets sorts and deduplicates by hour, keeps the last
// observation of each hour, drops samples outside the 7-day window and drops
// any observation without a used-byte value. Reporting frequency therefore
// cannot manufacture confidence: only the hourly point counts.
func normaliseForecastBuckets(buckets []ForecastBucket, totalBytes int64, now time.Time) []ForecastBucket {
	if len(buckets) == 0 {
		return nil
	}
	sorted := make([]ForecastBucket, 0, len(buckets))
	for _, bucket := range buckets {
		if bucket.UsedBytes < 0 || bucket.UsedBytes > totalBytes {
			continue
		}
		at := bucket.At.UTC().Truncate(time.Hour)
		if at.After(now) || at.Before(now.Add(-forecastWindow)) {
			continue
		}
		sorted = append(sorted, ForecastBucket{At: at, UsedBytes: bucket.UsedBytes})
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	out := make([]ForecastBucket, 0, len(sorted))
	for _, bucket := range sorted {
		if len(out) > 0 && out[len(out)-1].At.Equal(bucket.At) {
			out[len(out)-1] = bucket
			continue
		}
		out = append(out, bucket)
	}
	return out
}

// linearFit returns slope (bytes per hour) and R-squared. Zero variance is a
// perfect flat fit rather than a NaN.
func linearFit(points []forecastPoint) (slope, rSquared float64) {
	if len(points) < 2 {
		return 0, 0
	}
	n := float64(len(points))
	var sumX, sumY float64
	for _, point := range points {
		sumX += point.x
		sumY += point.y
	}
	meanX, meanY := sumX/n, sumY/n
	var cov, varX, varY float64
	for _, point := range points {
		dx := point.x - meanX
		dy := point.y - meanY
		cov += dx * dy
		varX += dx * dx
		varY += dy * dy
	}
	if varX == 0 {
		return 0, 1
	}
	slope = cov / varX
	if varY == 0 {
		return slope, 1
	}
	intercept := meanY - slope*meanX
	var residual float64
	for _, point := range points {
		expected := slope*point.x + intercept
		residual += (point.y - expected) * (point.y - expected)
	}
	rSquared = 1 - residual/varY
	if rSquared < 0 {
		rSquared = 0
	}
	if rSquared > 1 {
		rSquared = 1
	}
	return slope, rSquared
}

func availableBytes(totalBytes, usedBytes int64) uint64 {
	available := totalBytes - usedBytes
	if available < 0 {
		return 0
	}
	return uint64(available)
}

// forecastSeverity applies the confirmation hysteresis: warn inside 7 days,
// critical inside 24 hours, keep critical until it is clearly above 48 hours,
// downgrade to warning, and clear above 14 days.
func forecastSeverity(horizon time.Duration, current incidents.Severity) (incidents.Severity, bool) {
	switch {
	case horizon <= forecastCritical:
		return incidents.SeverityCritical, true
	case current == incidents.SeverityCritical && horizon <= forecastDowngrade:
		return incidents.SeverityCritical, true
	case horizon <= forecastWarning:
		return incidents.SeverityWarning, true
	case horizon <= forecastClear && current == incidents.SeverityWarning:
		return incidents.SeverityWarning, true
	default:
		return "", false
	}
}

// EvaluateForecasts runs once per hour: it reads the retained hourly buckets
// per filesystem (never raw samples), computes one estimate and reports it to
// the existing incident lifecycle.
func (s *Service) EvaluateForecasts(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	now := s.now().UTC()
	identities, err := s.store.ListFilesystemIdentities(ctx)
	if err != nil {
		return fmt.Errorf("monitoring: list filesystems for forecast: %w", err)
	}
	active, err := s.incidents.ActiveNodeIncidents(ctx)
	if err != nil {
		return fmt.Errorf("monitoring: list active incidents for forecast: %w", err)
	}
	rulesByNode := make(map[uuid.UUID]Rules)
	for _, identity := range identities {
		nodeID := uuid.UUID(identity.NodeID.Bytes)
		if nodeID == uuid.Nil {
			continue
		}
		rules, ok := rulesByNode[nodeID]
		if !ok {
			rules, err = s.effectiveRules(ctx, nodeID)
			if err != nil {
				s.logger.Warn("monitoring: resolve forecast policy",
					"node_id", nodeID.String(), "error", err)
				continue
			}
			rulesByNode[nodeID] = rules
		}
		key := filesystemIdentity(DetectorForecast, nodeID, identity.FilesystemKey)
		if !rules.Forecast {
			s.closeRuleDisabled(ctx, key)
			continue
		}
		forecast, err := s.filesystemForecast(ctx, identity, now)
		if err != nil {
			s.logger.Warn("monitoring: estimate filesystem forecast",
				"node_id", nodeID.String(), "path", identity.Path, "error", err)
			continue
		}
		s.reportForecast(ctx, nodeID, identity, key, forecast, now, active)
	}
	return nil
}

func (s *Service) filesystemForecast(ctx context.Context, identity sqlc.FilesystemIdentity, now time.Time) (FilesystemForecast, error) {
	rows, err := s.store.ListFilesystemUsageBuckets(ctx, sqlc.ListFilesystemUsageBucketsParams{
		IdentityID: identity.ID,
		Segment:    identity.Segment,
		BucketAt:   pgtype.Timestamptz{Time: now.Add(-forecastWindow), Valid: true},
	})
	if err != nil {
		return FilesystemForecast{}, err
	}
	return EstimateForecast(forecastBuckets(rows), identity.TotalBytes, now), nil
}

// nodeForecasts returns the current estimate per mount path for the server
// details card. It reads the same bounded hourly buckets as the hourly sweep.
func (s *Service) nodeForecasts(ctx context.Context, nodeID uuid.UUID, now time.Time) map[string]FilesystemForecast {
	if s == nil || s.store == nil {
		return nil
	}
	rules, err := s.effectiveRules(ctx, nodeID)
	if err != nil || !rules.Forecast {
		return nil
	}
	rows, err := s.store.ListFilesystemIdentitiesByNode(ctx, pgUUID(nodeID))
	if err != nil {
		return nil
	}
	out := make(map[string]FilesystemForecast, len(rows))
	for _, identity := range rows {
		forecast, err := s.filesystemForecast(ctx, identity, now)
		if err != nil {
			continue
		}
		out[identity.Path] = forecast
	}
	return out
}

func forecastBuckets(rows []sqlc.FilesystemUsageBucket) []ForecastBucket {
	out := make([]ForecastBucket, 0, len(rows))
	for _, row := range rows {
		if row.FreshSamples <= 0 || row.LastUsedBytes == nil || !row.BucketAt.Valid {
			continue
		}
		out = append(out, ForecastBucket{At: row.BucketAt.Time.UTC(), UsedBytes: *row.LastUsedBytes})
	}
	return out
}

func (s *Service) reportForecast(ctx context.Context, nodeID uuid.UUID, identity sqlc.FilesystemIdentity, key string, forecast FilesystemForecast, now time.Time, active []incidents.Incident) {
	obs := incidents.Observation{
		Identity: key, Detector: DetectorForecast, ScopeKind: incidents.ScopeNode,
		ScopeID: nodeID, NodeID: nodeID, ResourceType: ResourceFilesystem,
		ResourceID: uuid.UUID(identity.ID.Bytes), ResourceName: identity.Path, At: now,
		MaxGap: forecastMaxGap, ConfirmFor: forecastConfirm, RecoverFor: forecastRecover,
	}
	switch forecast.Status {
	case ForecastInsufficient:
		obs.Valid = false
		obs.Evidence = fmt.Sprintf(
			"Forecast needs at least 24 hours of hourly history with 12 valid buckets and 75%% coverage; %d buckets over %dh (%s available).",
			forecast.Samples, forecast.SpanHours, formatBytes(forecast.AvailableBytes))
	case ForecastStale:
		obs.Valid = false
		obs.Evidence = "Forecast history is stale: the latest hourly sample is older than 2 hours."
	case ForecastLowConfidence:
		obs.Valid = false
		obs.Evidence = fmt.Sprintf(
			"Filesystem use is too irregular to forecast (fit %.2f, needs %.2f); %s available.",
			forecast.Fit, forecastMinFit, formatBytes(forecast.AvailableBytes))
	case ForecastNonGrowing:
		obs.Valid = true
		obs.Evidence = fmt.Sprintf(
			"Filesystem use is not growing at the current rate (fit %.2f, %dh window, %d hourly buckets); %s available.",
			forecast.Fit, forecast.SpanHours, forecast.Samples, formatBytes(forecast.AvailableBytes))
	case ForecastEstimated:
		obs.Valid = true
		obs.Evidence = forecastEvidence(identity.Path, forecast)
		var current incidents.Severity
		for _, inc := range active {
			if inc.Identity == key {
				current = inc.Severity
				break
			}
		}
		severity, unhealthy := forecastSeverity(horizonDuration(forecast.HorizonHours), current)
		if unhealthy {
			obs.Unhealthy = true
			obs.Severity = severity
			// One disk problem is one external notification: when the fixed
			// threshold already alerts for this filesystem, keep the forecast
			// durable but group its delivery under that incident.
			for _, inc := range active {
				if inc.Detector == DetectorSpace && inc.ResourceType == ResourceFilesystem &&
					inc.ResourceID == obs.ResourceID {
					obs.SuppressDelivery = true
					obs.SuppressReason = "grouped under disk space alert"
					break
				}
			}
		}
	default:
		return
	}
	_, _, err := s.incidents.Report(ctx, obs)
	s.logReport(DetectorForecast, nodeID, err)
}

func forecastEvidence(path string, forecast FilesystemForecast) string {
	return fmt.Sprintf(
		"At the current rate, %s fills in about %s (fit %.2f, %dh window, %d hourly buckets, %s available).",
		path, formatHorizon(horizonDuration(forecast.HorizonHours)), forecast.Fit,
		forecast.SpanHours, forecast.Samples, formatBytes(forecast.AvailableBytes))
}

func horizonDuration(hours float64) time.Duration {
	if hours <= 0 {
		return 0
	}
	if hours > forecastMaxHorizonHrs {
		hours = forecastMaxHorizonHrs
	}
	return time.Duration(hours * float64(time.Hour))
}

// formatHorizon avoids a precise guaranteed date: it rounds to the precision a
// trend can support (hours below two days, then days).
func formatHorizon(horizon time.Duration) string {
	hours := int(horizon.Round(time.Hour) / time.Hour)
	if hours < 48 {
		return fmt.Sprintf("%dh", hours)
	}
	days := float64(hours) / 24
	if days < 10 {
		return fmt.Sprintf("%.1fd", days)
	}
	return fmt.Sprintf("%.0fd", days)
}

func formatBytes(bytes uint64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(bytes)
	unit := "B"
	for _, next := range units {
		value /= 1024
		unit = next
		if value < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func timePtr(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}
