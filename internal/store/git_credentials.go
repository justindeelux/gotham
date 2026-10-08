package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// UpsertApplicationGitCredential stores (or rotates) an application's sealed
// HTTPS token for git_private sources (GS-4). The token arrives already
// sealed with providers.SealSecret; it is opened only by the cloner and the
// connection test.
func (s *Store) UpsertApplicationGitCredential(ctx context.Context, applicationID pgtype.UUID, username, ciphertext string) (sqlc.ApplicationGitCredential, error) {
	return s.queries.UpsertApplicationGitCredential(ctx, sqlc.UpsertApplicationGitCredentialParams{
		ApplicationID: applicationID,
		Username:      username,
		Ciphertext:    ciphertext,
	})
}

// GetApplicationGitCredential returns an application's HTTPS credential row,
// or pgx.ErrNoRows when none is set.
func (s *Store) GetApplicationGitCredential(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationGitCredential, error) {
	return s.queries.GetApplicationGitCredential(ctx, applicationID)
}

// DeleteApplicationGitCredential removes an application's HTTPS credential
// row. It is idempotent: a missing row is not an error.
func (s *Store) DeleteApplicationGitCredential(ctx context.Context, applicationID pgtype.UUID) error {
	return s.queries.DeleteApplicationGitCredential(ctx, applicationID)
}
