package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) SaveFileVersion(ctx context.Context, params sqlc.InsertFileVersionParams, maxVersions, maxBytes int64) (sqlc.InsertFileVersionRow, error) {
	var row sqlc.InsertFileVersionRow
	err := s.InTx(ctx, func(tx *Store) error {
		inserted, err := tx.queries.InsertFileVersion(ctx, params)
		if err != nil {
			return err
		}
		row = inserted
		prune := sqlc.PruneFileVersionsByCountParams{
			ServerID:    params.ServerID,
			Root:        params.Root,
			Path:        params.Path,
			MaxVersions: maxVersions,
		}
		if err := tx.queries.PruneFileVersionsByCount(ctx, prune); err != nil {
			return err
		}
		return tx.queries.PruneFileVersionsBySize(ctx, sqlc.PruneFileVersionsBySizeParams{
			ServerID: params.ServerID,
			Root:     params.Root,
			Path:     params.Path,
			MaxBytes: maxBytes,
		})
	})
	if err != nil {
		return sqlc.InsertFileVersionRow{}, err
	}
	return row, nil
}

func (s *Store) ListFileVersions(ctx context.Context, params sqlc.ListFileVersionsParams) ([]sqlc.ListFileVersionsRow, error) {
	return s.queries.ListFileVersions(ctx, params)
}

func (s *Store) GetFileVersion(ctx context.Context, params sqlc.GetFileVersionParams) (sqlc.GetFileVersionRow, error) {
	return s.queries.GetFileVersion(ctx, params)
}

func (s *Store) DeleteFileVersion(ctx context.Context, params sqlc.DeleteFileVersionParams) (bool, error) {
	removed, err := s.queries.DeleteFileVersion(ctx, params)
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}
