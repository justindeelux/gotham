package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// File version persistence (JUS-224). A snapshot is the pre-save body of one
// file on one node, addressed by the same (server, root, path) tuple as the
// file routes. Retention is applied in the inserting transaction, so the table
// never holds more than maxVersions rows or maxBytes of bodies per file.

// SaveFileVersion records one snapshot and prunes the file's older revisions
// (by count, then by bytes) in one transaction.
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

// ListFileVersions returns one file's revisions, newest first.
func (s *Store) ListFileVersions(ctx context.Context, params sqlc.ListFileVersionsParams) ([]sqlc.ListFileVersionsRow, error) {
	return s.queries.ListFileVersions(ctx, params)
}

// GetFileVersion reads one revision scoped to its file key.
func (s *Store) GetFileVersion(ctx context.Context, params sqlc.GetFileVersionParams) (sqlc.GetFileVersionRow, error) {
	return s.queries.GetFileVersion(ctx, params)
}

// DeleteFileVersion removes one revision, reporting false when the row does not
// belong to the addressed file (or is already gone).
func (s *Store) DeleteFileVersion(ctx context.Context, params sqlc.DeleteFileVersionParams) (bool, error) {
	removed, err := s.queries.DeleteFileVersion(ctx, params)
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}
