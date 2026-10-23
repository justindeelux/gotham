package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

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
	app, err := queries.GetApplication(ctx, params.ApplicationID)
	if err != nil {
		return sqlc.ApplicationDeployKey{}, fmt.Errorf("store: get deploy-key application: %w", err)
	}
	keyRow, err := queries.CreatePrivateKey(ctx, sqlc.CreatePrivateKeyParams{
		Name:         privateKeyName,
		EncryptedKey: encryptedKey,
		TeamID:       app.TeamID,
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

func (s *Store) GetApplicationDeployKey(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationDeployKey, error) {
	return s.queries.GetApplicationDeployKey(ctx, applicationID)
}

func (s *Store) DeleteApplicationDeployKey(ctx context.Context, keyID, applicationID pgtype.UUID) (sqlc.ApplicationDeployKey, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.ApplicationDeployKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	row, err := queries.DeleteApplicationDeployKey(ctx, sqlc.DeleteApplicationDeployKeyParams{
		ID:            keyID,
		ApplicationID: applicationID,
	})
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

func (s *Store) DeletePrivateKey(ctx context.Context, privateKeyID pgtype.UUID) error {
	return s.queries.DeletePrivateKey(ctx, privateKeyID)
}
