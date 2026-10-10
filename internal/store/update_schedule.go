package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// GetUpdateSchedule returns the persisted self-update schedule, or
// pgx.ErrNoRows when none has been saved (callers fall back to the env defaults).
func (s *Store) GetUpdateSchedule(ctx context.Context) (sqlc.GetUpdateScheduleRow, error) {
	return s.queries.GetUpdateSchedule(ctx)
}

// UpsertUpdateSchedule stores the single self-update schedule row.
func (s *Store) UpsertUpdateSchedule(ctx context.Context, params sqlc.UpsertUpdateScheduleParams) (sqlc.UpsertUpdateScheduleRow, error) {
	return s.queries.UpsertUpdateSchedule(ctx, params)
}
