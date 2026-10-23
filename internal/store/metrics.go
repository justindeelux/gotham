package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) InsertServerMetric(ctx context.Context, params sqlc.InsertServerMetricParams) error {
	return s.queries.InsertServerMetric(ctx, params)
}

func (s *Store) ListServerMetrics(ctx context.Context, params sqlc.ListServerMetricsParams) ([]sqlc.ListServerMetricsRow, error) {
	return s.queries.ListServerMetrics(ctx, params)
}

func (s *Store) DeleteServerMetricsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.queries.DeleteServerMetricsBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}
