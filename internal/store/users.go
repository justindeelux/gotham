package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateUser(ctx context.Context, email string, passwordHash *string) (sqlc.User, error) {
	return s.queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
}

const firstUserLockKey = int64(0x676f7468616d)

var ErrInstanceHasAccount = errors.New("store: instance already has an account")

func (s *Store) CreateFirstUser(ctx context.Context, email string, passwordHash *string) (sqlc.User, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", firstUserLockKey); err != nil {
		return sqlc.User{}, err
	}

	queries := s.queries.WithTx(tx)
	user, err := queries.CreateFirstUser(ctx, sqlc.CreateFirstUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, ErrInstanceHasAccount
		}
		return sqlc.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, err
	}
	return user, nil
}

func (s *Store) DeleteUser(ctx context.Context, userID pgtype.UUID) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
	return err
}
