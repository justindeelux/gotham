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

const FeatureEnv = "FEATURE_METRICS"

func MetricsEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const (
	MetricsRetention  = 30 * 24 * time.Hour
	DefaultMetricStep = "1m"
)

type metricStep struct {
	bucket    time.Duration
	maxWindow time.Duration
}

var metricSteps = map[string]metricStep{
	"1m": {bucket: time.Minute, maxWindow: 24 * time.Hour},
	"1h": {bucket: time.Hour, maxWindow: MetricsRetention},
	"1d": {bucket: 24 * time.Hour, maxWindow: MetricsRetention},
}

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

const heartbeatSampleInterval = 5 * time.Second

const metricClockSkew = 5 * time.Minute

func (s *ServerService) claimMetricSample(nodeID string, now time.Time) bool {
	s.metricMu.Lock()
	defer s.metricMu.Unlock()
	if last, ok := s.lastMetricAt[nodeID]; ok && now.Sub(last) < heartbeatSampleInterval {
		return false
	}
	s.lastMetricAt[nodeID] = now
	return true
}

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
