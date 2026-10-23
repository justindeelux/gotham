package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetUpdateSchedule(ctx context.Context) (sqlc.GetUpdateScheduleRow, error) {
	return s.queries.GetUpdateSchedule(ctx)
}

func (s *Store) UpsertUpdateSchedule(ctx context.Context, params sqlc.UpsertUpdateScheduleParams) (sqlc.UpsertUpdateScheduleRow, error) {
	return s.queries.UpsertUpdateSchedule(ctx, params)
}
