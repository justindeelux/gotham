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

// UpdateDatabaseName persists only the name and returns the fresh row. The
// update is fenced on the row still being live, so a rename cannot follow a
// soft delete back into visibility, and it cannot clobber a concurrent status
// or container-id write.
func (s *Store) UpdateDatabaseName(ctx context.Context, params sqlc.UpdateDatabaseNameParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseName(ctx, params)
}

// UpdateDatabaseContainer persists only the container id and returns the
// fresh row. The live-row fence is what stops a container id recorded after a
// delete from resurrecting the row on reads.
func (s *Store) UpdateDatabaseContainer(ctx context.Context, params sqlc.UpdateDatabaseContainerParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseContainer(ctx, params)
}

// UpdateDatabaseStatus persists only the status and returns the fresh row;
// like the other scoped writes it is fenced on the row being live.
func (s *Store) UpdateDatabaseStatus(ctx context.Context, params sqlc.UpdateDatabaseStatusParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseStatus(ctx, params)
}

// PublicPortInUse reports whether any live database already publishes
// publicPort on serverID, so create can reject a port the node would refuse.
func (s *Store) PublicPortInUse(ctx context.Context, params sqlc.PublicPortInUseParams) (bool, error) {
	return s.queries.PublicPortInUse(ctx, params)
}

// DeleteDatabaseSecrets removes every sealed credential of a database. It is
// the create path's rollback for a partial credential write.
func (s *Store) DeleteDatabaseSecrets(ctx context.Context, databaseID pgtype.UUID) error {
	return s.queries.DeleteDatabaseSecrets(ctx, databaseID)
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
