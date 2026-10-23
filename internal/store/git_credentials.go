package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) UpsertApplicationGitCredential(ctx context.Context, applicationID pgtype.UUID, username, ciphertext string) (sqlc.ApplicationGitCredential, error) {
	return s.queries.UpsertApplicationGitCredential(ctx, sqlc.UpsertApplicationGitCredentialParams{
		ApplicationID: applicationID,
		Username:      username,
		Ciphertext:    ciphertext,
	})
}

func (s *Store) GetApplicationGitCredential(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationGitCredential, error) {
	return s.queries.GetApplicationGitCredential(ctx, applicationID)
}

func (s *Store) DeleteApplicationGitCredential(ctx context.Context, applicationID pgtype.UUID) error {
	return s.queries.DeleteApplicationGitCredential(ctx, applicationID)
}
