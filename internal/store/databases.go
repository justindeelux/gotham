package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateDatabase stores a database row with the caller-generated ID (the ID
// seeds the named volume) and returns it.
func (s *Store) CreateDatabase(ctx context.Context, params sqlc.CreateDatabaseParams) (sqlc.Database, error) {
	params.TeamID = personalTeamOrDefault(params.TeamID, params.UserID)
	return s.queries.CreateDatabase(ctx, params)
}

// GetDatabase returns a live database (soft-deleted rows are invisible) with
// the given ID; ownership is checked by the caller.
func (s *Store) GetDatabase(ctx context.Context, id pgtype.UUID) (sqlc.Database, error) {
	return s.queries.GetDatabase(ctx, id)
}

// ListDatabasesByUser returns every live database owned by userID, newest
// first.
func (s *Store) ListDatabasesByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Database, error) {
	return s.queries.ListDatabasesByUser(ctx, userID)
}

// ListDatabasesByTeam returns every live database of one team, newest first.
func (s *Store) ListDatabasesByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Database, error) {
	return s.queries.ListDatabasesByTeam(ctx, teamID)
}

// UpdateDatabase persists the mutable database fields (rename, status,
// container id) and returns the row.
func (s *Store) UpdateDatabase(ctx context.Context, params sqlc.UpdateDatabaseParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabase(ctx, params)
}

// SoftDeleteDatabase marks a database deleted without touching its volume, so
// the data survives the grace window. An already-deleted row reports
// pgx.ErrNoRows.
func (s *Store) SoftDeleteDatabase(ctx context.Context, id pgtype.UUID) (sqlc.Database, error) {
	return s.queries.SoftDeleteDatabase(ctx, id)
}

// ListExpiredDatabases returns soft-deleted databases whose grace window ended
// at or before the cutoff, oldest deletion first. It is the only read that sees
// deleted rows and backs the retention sweep.
func (s *Store) ListExpiredDatabases(ctx context.Context, deletedAt pgtype.Timestamptz) ([]sqlc.Database, error) {
	return s.queries.ListExpiredDatabases(ctx, deletedAt)
}

// PurgeDatabase hard-deletes a soft-deleted database (and, by cascade, its
// sealed credentials) and reports how many rows were removed. A live row is
// never touched, so a purge can never delete a database that was recreated.
func (s *Store) PurgeDatabase(ctx context.Context, id pgtype.UUID) (int64, error) {
	return s.queries.PurgeDatabase(ctx, id)
}

// CreateDatabaseSecret stores one sealed credential of a database and returns
// the row. The ciphertext is opened by the databases service, never here.
func (s *Store) CreateDatabaseSecret(ctx context.Context, params sqlc.CreateDatabaseSecretParams) (sqlc.DatabaseSecret, error) {
	return s.queries.CreateDatabaseSecret(ctx, params)
}

// ListDatabaseSecrets returns a database's sealed credentials, sorted by key.
func (s *Store) ListDatabaseSecrets(ctx context.Context, databaseID pgtype.UUID) ([]sqlc.DatabaseSecret, error) {
	return s.queries.ListDatabaseSecrets(ctx, databaseID)
}
