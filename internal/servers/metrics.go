package servers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// FeatureEnv is the kill switch for the metrics surface: FEATURE_METRICS=false
// unmounts the range route and stops heartbeat samples from being persisted,
// while the node registry and every other servers route stay untouched.
const FeatureEnv = "FEATURE_METRICS"

// MetricsEnabled reports whether host metrics are collected. Only an explicit
// false disables them — unset (or any other value) keeps the feature on,
// matching services.Enabled and the other flags.
func MetricsEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// Metrics retention and aggregation defaults.
const (
	// MetricsRetention is how long a sample is kept. Older rows are removed by
	// the periodic sweep, never by the heartbeat path.
	MetricsRetention = 30 * 24 * time.Hour
	// DefaultMetricStep is the bucket of a range query that names no step.
	DefaultMetricStep = "1m"
)

// metricStep describes one accepted step value: the aggregation bucket and the
// largest window a single request may span at that resolution, so a query can
// never materialize an unbounded series (a 1m window caps at 1440 points, a 1h
// window at 720, a 1d window at 30).
type metricStep struct {
	bucket    time.Duration
	maxWindow time.Duration
}

// metricSteps maps the accepted step names to their resolution.
var metricSteps = map[string]metricStep{
	"1m": {bucket: time.Minute, maxWindow: 24 * time.Hour},
	"1h": {bucket: time.Hour, maxWindow: MetricsRetention},
	"1d": {bucket: 24 * time.Hour, maxWindow: MetricsRetention},
}

// MetricPoint is one aggregated bucket of a server's time series. Bucket
// timestamps are UTC and aligned to the epoch, so a 1m bucket is a wall-clock
// UTC minute. ContainerCount is an average, hence fractional.
type MetricPoint struct {
	Bucket         time.Time
	CPUUsage       float64
	MemUsage       float64
	DiskUsage      float64
	NetRxBps       float64
	NetTxBps       float64
	DiskReadBps    float64
	DiskWriteBps   float64
	ContainerCount float64
}

// ParseMetricsQuery validates a range query and returns the bucket duration the
// series must be aggregated with. from and to are required and must satisfy
// from < to; step is one of 1m, 1h, 1d and defaults to 1m. A window larger than
// the step's cap is rejected so one request cannot ask for an unbounded series.
func ParseMetricsQuery(from, to time.Time, step string) (time.Duration, error) {
	if step == "" {
		step = DefaultMetricStep
	}
	spec, ok := metricSteps[step]
	if !ok {
		return 0, fmt.Errorf("%w: step must be one of 1m, 1h or 1d", ErrValidation)
	}
	if from.IsZero() || to.IsZero() {
		return 0, fmt.Errorf("%w: from and to are required", ErrValidation)
	}
	if !from.Before(to) {
		return 0, fmt.Errorf("%w: from must be before to", ErrValidation)
	}
	if window := to.Sub(from); window > spec.maxWindow {
		return 0, fmt.Errorf("%w: range for step %s must not exceed %s", ErrValidation, step, spec.maxWindow)
	}
	return spec.bucket, nil
}

// Metrics returns the aggregated time series of one server for the
// [from, to) window, one point per non-empty bucket, oldest first. Buckets
// without samples are omitted: the series carries gaps rather than inventing
// zero points. Read access follows the node registry: a node of another team is
// ErrNotFound, a read_only member of the node's team may read.
func (s *ServerService) Metrics(ctx context.Context, id uuid.UUID, from, to time.Time, step string) ([]MetricPoint, error) {
	if s.store == nil {
		return nil, errors.New("servers: store is not configured")
	}
	bucket, err := ParseMetricsQuery(from, to, step)
	if err != nil {
		return nil, err
	}
	server, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	rows, err := s.store.ListServerMetrics(ctx, sqlc.ListServerMetricsParams{
		ServerID:   pgUUID(server.ID),
		Bucket:     pgtype.Interval{Microseconds: bucket.Microseconds(), Valid: true},
		RangeStart: pgtype.Timestamptz{Time: from, Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list server metrics: %w", err)
	}

	points := make([]MetricPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, MetricPoint{
			Bucket:         row.Bucket.Time.UTC(),
			CPUUsage:       row.CpuUsage,
			MemUsage:       row.MemUsage,
			DiskUsage:      row.DiskUsage,
			NetRxBps:       row.NetRxBps,
			NetTxBps:       row.NetTxBps,
			DiskReadBps:    row.DiskReadBps,
			DiskWriteBps:   row.DiskWriteBps,
			ContainerCount: row.ContainerCount,
		})
	}
	return points, nil
}

// heartbeatSampleInterval is the minimum wall-clock gap between persisted
// time-series samples for one node. An unauthenticated peer cannot grow
// server_metrics faster than this no matter how many heartbeat messages it
// sends; the live snapshot on the servers row still updates on every message.
const heartbeatSampleInterval = 10 * time.Second

// metricClockSkew bounds how far a heartbeat's self-reported sent_at may differ
// from the server clock before it is ignored for storage. A peer cannot push a
// sample into the far future (which would poison the series and, previously,
// the throttle) or the distant past.
const metricClockSkew = 5 * time.Minute

// claimMetricSample reports whether a sample arriving at now should be
// persisted, and records now as the node's latest. The decision uses the
// server's wall clock, never the peer-supplied sent_at, so a peer that advances
// sent_at cannot bypass the throttle. The map is bounded by the node registry;
// entries for deleted nodes are not reclaimed, which is acceptable at registry
// scale.
func (s *ServerService) claimMetricSample(nodeID string, now time.Time) bool {
	s.metricMu.Lock()
	defer s.metricMu.Unlock()
	if last, ok := s.lastMetricAt[nodeID]; ok && now.Sub(last) < heartbeatSampleInterval {
		return false
	}
	s.lastMetricAt[nodeID] = now
	return true
}

// recordMetric appends one heartbeat sample to the time series. It is
// best-effort by contract: the servers row already carries the latest snapshot,
// so a failed append is logged and the next heartbeat tries again.
// FEATURE_METRICS=false skips the append entirely. The sample is stored at the
// heartbeat's sent_at so the series is the node's own clock, clamped to the
// server clock when the report is outside metricClockSkew; a heartbeat without
// one is stamped on arrival. Arrival times closer together than
// heartbeatSampleInterval are dropped server-side so a flood cannot fill the
// table.
func (s *ServerService) recordMetric(ctx context.Context, serverID pgtype.UUID, req *agentv1.HeartbeatRequest) {
	if !MetricsEnabled() {
		return
	}
	now := s.now().UTC()
	recordedAt := now
	if sentAt := req.GetSentAt(); sentAt != nil {
		if at := sentAt.AsTime().UTC(); !at.Before(now.Add(-metricClockSkew)) && !at.After(now.Add(metricClockSkew)) {
			recordedAt = at
		}
	}
	if !s.claimMetricSample(serverID.String(), now) {
		return
	}
	err := s.store.InsertServerMetric(ctx, sqlc.InsertServerMetricParams{
		ServerID:       serverID,
		RecordedAt:     pgtype.Timestamptz{Time: recordedAt, Valid: true},
		CpuUsage:       req.GetCpuUsage(),
		MemUsage:       req.GetMemUsage(),
		DiskUsage:      req.GetDiskUsage(),
		NetRxBps:       req.GetNetRxBps(),
		NetTxBps:       req.GetNetTxBps(),
		DiskReadBps:    req.GetDiskReadBps(),
		DiskWriteBps:   req.GetDiskWriteBps(),
		ContainerCount: req.GetContainerCount(),
	})
	if err != nil {
		s.logger.Warn("servers: recording metric sample failed",
			"server_id", serverID.String(), "error", err)
	}
}
