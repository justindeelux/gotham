package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateProvider(ctx context.Context, params sqlc.CreateProviderParams) (sqlc.Provider, error) {
	return s.queries.CreateProvider(ctx, params)
}

func (s *Store) GetProviderByIDAndUser(ctx context.Context, params sqlc.GetProviderByIDAndUserParams) (sqlc.Provider, error) {
	return s.queries.GetProviderByIDAndUser(ctx, params)
}

func (s *Store) ListProvidersByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Provider, error) {
	return s.queries.ListProvidersByUser(ctx, userID)
}

func (s *Store) UpdateProviderToken(ctx context.Context, params sqlc.UpdateProviderTokenParams) (sqlc.Provider, error) {
	return s.queries.UpdateProviderToken(ctx, params)
}

func (s *Store) DeleteProviderByIDAndUser(ctx context.Context, params sqlc.DeleteProviderByIDAndUserParams) error {
	return s.queries.DeleteProviderByIDAndUser(ctx, params)
}

func (s *Store) UpsertRepoCache(ctx context.Context, params sqlc.UpsertRepoCacheParams) (sqlc.ReposCache, error) {
	return s.queries.UpsertRepoCache(ctx, params)
}

func (s *Store) ListRepoCacheByProvider(ctx context.Context, providerID pgtype.UUID) ([]sqlc.ReposCache, error) {
	return s.queries.ListRepoCacheByProvider(ctx, providerID)
}

func (s *Store) DeleteRepoCacheByProvider(ctx context.Context, providerID pgtype.UUID) error {
	return s.queries.DeleteRepoCacheByProvider(ctx, providerID)
}

func (s *Store) ReplaceRepoCache(ctx context.Context, providerID pgtype.UUID, repos []sqlc.UpsertRepoCacheParams) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked pgtype.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM providers WHERE id = $1 FOR UPDATE", providerID).Scan(&locked); err != nil {
		return err
	}

	queries := s.queries.WithTx(tx)
	if err := queries.DeleteRepoCacheByProvider(ctx, providerID); err != nil {
		return err
	}
	if s.BeforeRepoCacheInsert != nil {
		if err := s.BeforeRepoCacheInsert(); err != nil {
			return err
		}
	}
	for _, repo := range repos {
		if _, err := queries.UpsertRepoCache(ctx, repo); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
