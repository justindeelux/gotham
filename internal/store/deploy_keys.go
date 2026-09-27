package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateApplicationDeployKey stores an application's deploy key: the sealed
// private key in private_keys plus the application_deploy_keys mapping that
// points at it, in one transaction. A mapping must never exist without its
// private key (the cloner would open nothing), so the two rows are written
// together.
func (s *Store) CreateApplicationDeployKey(
	ctx context.Context,
	params sqlc.CreateApplicationDeployKeyParams,
	privateKeyName, encryptedKey string,
) (sqlc.ApplicationDeployKey, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	keyRow, err := queries.CreatePrivateKey(ctx, sqlc.CreatePrivateKeyParams{
		Name:         privateKeyName,
		EncryptedKey: encryptedKey,
	})
	if err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	params.PrivateKeyID = keyRow.ID
	row, err := queries.CreateApplicationDeployKey(ctx, params)
	if err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	return row, nil
}

// GetApplicationDeployKey returns the deploy key of an application, or
// pgx.ErrNoRows when the application has none.
func (s *Store) GetApplicationDeployKey(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationDeployKey, error) {
	return s.queries.GetApplicationDeployKey(ctx, applicationID)
}

// DeleteApplicationDeployKey removes an application's deploy key: the mapping
// row and the private key it points at, in one transaction, and returns the
// mapping that was removed (pgx.ErrNoRows when there was nothing to delete).
// Deleting the private key first would cascade the mapping away, so the
// mapping is read back explicitly before both rows go.
func (s *Store) DeleteApplicationDeployKey(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationDeployKey, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	row, err := queries.DeleteApplicationDeployKey(ctx, applicationID)
	if err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	if err := queries.DeletePrivateKey(ctx, row.PrivateKeyID); err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	return row, nil
}
