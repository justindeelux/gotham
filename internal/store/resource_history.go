package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) InsertResourceSamples(ctx context.Context, params []sqlc.InsertResourceSamplesParams) error {
	if len(params) == 0 {
		return nil
	}
	_, err := s.queries.InsertResourceSamples(ctx, params)
	return err
}

func (s *Store) ListResourceSamples(ctx context.Context, params sqlc.ListResourceSamplesParams) ([]sqlc.ListResourceSamplesRow, error) {
	return s.queries.ListResourceSamples(ctx, params)
}

func (s *Store) ListResourceRollupPoints(ctx context.Context, params sqlc.ListResourceRollupPointsParams) ([]sqlc.ListResourceRollupPointsRow, error) {
	return s.queries.ListResourceRollupPoints(ctx, params)
}

func (s *Store) UpsertResourceMinuteRollups(ctx context.Context, from, to time.Time) (int64, error) {
	return s.queries.UpsertResourceMinuteRollups(ctx, sqlc.UpsertResourceMinuteRollupsParams{
		RangeStart: pgtype.Timestamptz{Time: from, Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: to, Valid: true},
	})
}

func (s *Store) UpsertResourceHourRollups(ctx context.Context, from, to time.Time) (int64, error) {
	return s.queries.UpsertResourceHourRollups(ctx, sqlc.UpsertResourceHourRollupsParams{
		RangeStart: pgtype.Timestamptz{Time: from, Valid: true},
		RangeEnd:   pgtype.Timestamptz{Time: to, Valid: true},
	})
}

func (s *Store) DeleteResourceSamplesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.queries.DeleteResourceSamplesBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}

func (s *Store) DeleteResourceRollupsBefore(ctx context.Context, tier string, cutoff time.Time) (int64, error) {
	return s.queries.DeleteResourceRollupsBefore(ctx, sqlc.DeleteResourceRollupsBeforeParams{
		Tier:   tier,
		Bucket: pgtype.Timestamptz{Time: cutoff, Valid: true},
	})
}

func (s *Store) DeleteResourceHistory(ctx context.Context, resourceType string, resourceID pgtype.UUID) error {
	params := sqlc.DeleteResourceRollupHistoryParams{
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}
	if _, err := s.queries.DeleteResourceHistory(ctx, sqlc.DeleteResourceHistoryParams(params)); err != nil {
		return err
	}
	_, err := s.queries.DeleteResourceRollupHistory(ctx, params)
	return err
}

func (s *Store) ResourceHistoryExists(ctx context.Context, resourceType string, resourceID pgtype.UUID) (bool, error) {
	return s.queries.ResourceHistoryExists(ctx, sqlc.ResourceHistoryExistsParams{
		ResourceType: resourceType,
		ResourceID:   resourceID,
	})
}
