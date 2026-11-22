package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) ListOAuthProviders(ctx context.Context) ([]sqlc.OauthProvider, error) {
	return s.queries.ListOAuthProviders(ctx)
}

func (s *Store) UpsertOAuthProvider(ctx context.Context, params sqlc.UpsertOAuthProviderParams) (sqlc.OauthProvider, error) {
	return s.queries.UpsertOAuthProvider(ctx, params)
}
