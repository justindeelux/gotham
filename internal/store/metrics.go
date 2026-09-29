package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// InsertServerMetric appends one heartbeat sample (Phase 8, BE-8.4). A sample
// for an instant that is already stored is ignored, so a resent heartbeat is
// deduplicated rather than an error.
func (s *Store) InsertServerMetric(ctx context.Context, params sqlc.InsertServerMetricParams) error {
	return s.queries.InsertServerMetric(ctx, params)
}

// ListServerMetrics returns one aggregated row per non-empty bucket in
// [rangeStart, rangeEnd), oldest first.
func (s *Store) ListServerMetrics(ctx context.Context, params sqlc.ListServerMetricsParams) ([]sqlc.ListServerMetricsRow, error) {
	return s.queries.ListServerMetrics(ctx, params)
}

// DeleteServerMetricsBefore drops every sample recorded strictly before cutoff
// (the retention sweep) and reports how many rows were removed.
func (s *Store) DeleteServerMetricsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.queries.DeleteServerMetricsBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}
