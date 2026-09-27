package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateProvider stores a source-provider connection and returns the row. The
// credential fields carry ciphertext produced by the providers service.
func (s *Store) CreateProvider(ctx context.Context, params sqlc.CreateProviderParams) (sqlc.Provider, error) {
	return s.queries.CreateProvider(ctx, params)
}

// GetProviderByIDAndUser returns the provider with the given ID when it is
// owned by userID.
func (s *Store) GetProviderByIDAndUser(ctx context.Context, params sqlc.GetProviderByIDAndUserParams) (sqlc.Provider, error) {
	return s.queries.GetProviderByIDAndUser(ctx, params)
}

// ListProvidersByUser returns every provider connection owned by userID,
// newest first.
func (s *Store) ListProvidersByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Provider, error) {
	return s.queries.ListProvidersByUser(ctx, userID)
}

// UpdateProviderToken replaces the stored access/refresh tokens of a provider
// and returns the updated row.
func (s *Store) UpdateProviderToken(ctx context.Context, params sqlc.UpdateProviderTokenParams) (sqlc.Provider, error) {
	return s.queries.UpdateProviderToken(ctx, params)
}

// UpsertRepoCache inserts or refreshes one cached repository.
func (s *Store) UpsertRepoCache(ctx context.Context, params sqlc.UpsertRepoCacheParams) (sqlc.ReposCache, error) {
	return s.queries.UpsertRepoCache(ctx, params)
}

// ListRepoCacheByProvider returns a provider's cached repositories ordered by
// full name.
func (s *Store) ListRepoCacheByProvider(ctx context.Context, providerID pgtype.UUID) ([]sqlc.ReposCache, error) {
	return s.queries.ListRepoCacheByProvider(ctx, providerID)
}

// DeleteRepoCacheByProvider removes every cached repository of a provider.
func (s *Store) DeleteRepoCacheByProvider(ctx context.Context, providerID pgtype.UUID) error {
	return s.queries.DeleteRepoCacheByProvider(ctx, providerID)
}
