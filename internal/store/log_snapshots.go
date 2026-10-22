package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetBoundedBuildLog(ctx context.Context, params sqlc.GetBoundedBuildLogParams) (sqlc.GetBoundedBuildLogRow, error) {
	return s.queries.GetBoundedBuildLog(ctx, params)
}
