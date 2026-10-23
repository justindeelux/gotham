package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateDatabase(ctx context.Context, params sqlc.CreateDatabaseParams) (sqlc.Database, error) {
	params.TeamID = personalTeamOrDefault(params.TeamID, params.UserID)
	return s.queries.CreateDatabase(ctx, params)
}

func (s *Store) GetDatabase(ctx context.Context, id pgtype.UUID) (sqlc.Database, error) {
	return s.queries.GetDatabase(ctx, id)
}

func (s *Store) ListDatabasesByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Database, error) {
	return s.queries.ListDatabasesByUser(ctx, userID)
}

func (s *Store) ListDatabasesByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Database, error) {
	return s.queries.ListDatabasesByTeam(ctx, teamID)
}

func (s *Store) ListDatabasesByEnvironment(ctx context.Context, environmentID pgtype.UUID) ([]sqlc.Database, error) {
	return s.queries.ListDatabasesByEnvironment(ctx, environmentID)
}

func (s *Store) ListDatabasesByProject(ctx context.Context, projectID pgtype.UUID) ([]sqlc.Database, error) {
	return s.queries.ListDatabasesByProject(ctx, projectID)
}

func (s *Store) CountDatabasesByEnvironment(ctx context.Context, environmentID pgtype.UUID) (int64, error) {
	return s.queries.CountDatabasesByEnvironment(ctx, environmentID)
}

func (s *Store) CountDatabasesByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountDatabasesByProject(ctx, projectID)
}

func (s *Store) ListDatabasesByServer(ctx context.Context, serverID pgtype.UUID) ([]sqlc.ListDatabasesByServerRow, error) {
	return s.queries.ListDatabasesByServer(ctx, serverID)
}

func (s *Store) DatabaseNameInEnvironment(ctx context.Context, params sqlc.DatabaseNameInEnvironmentParams) (bool, error) {
	return s.queries.DatabaseNameInEnvironment(ctx, params)
}

func (s *Store) UpdateDatabaseTarget(ctx context.Context, params sqlc.UpdateDatabaseTargetParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseTarget(ctx, params)
}

func (s *Store) UpdateDatabaseName(ctx context.Context, params sqlc.UpdateDatabaseNameParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseName(ctx, params)
}

func (s *Store) UpdateDatabaseContainer(ctx context.Context, params sqlc.UpdateDatabaseContainerParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseContainer(ctx, params)
}

func (s *Store) UpdateDatabaseStatus(ctx context.Context, params sqlc.UpdateDatabaseStatusParams) (sqlc.Database, error) {
	return s.queries.UpdateDatabaseStatus(ctx, params)
}

func (s *Store) PublicPortInUse(ctx context.Context, params sqlc.PublicPortInUseParams) (bool, error) {
	return s.queries.PublicPortInUse(ctx, params)
}

func (s *Store) DeleteDatabaseSecrets(ctx context.Context, databaseID pgtype.UUID) error {
	return s.queries.DeleteDatabaseSecrets(ctx, databaseID)
}

func (s *Store) SoftDeleteDatabase(ctx context.Context, id pgtype.UUID) (sqlc.Database, error) {
	return s.queries.SoftDeleteDatabase(ctx, id)
}

func (s *Store) ListExpiredDatabases(ctx context.Context, deletedAt pgtype.Timestamptz) ([]sqlc.Database, error) {
	return s.queries.ListExpiredDatabases(ctx, deletedAt)
}

func (s *Store) PurgeDatabase(ctx context.Context, id pgtype.UUID) (int64, error) {
	return s.queries.PurgeDatabase(ctx, id)
}

func (s *Store) CreateDatabaseSecret(ctx context.Context, params sqlc.CreateDatabaseSecretParams) (sqlc.DatabaseSecret, error) {
	return s.queries.CreateDatabaseSecret(ctx, params)
}

func (s *Store) ListDatabaseSecrets(ctx context.Context, databaseID pgtype.UUID) ([]sqlc.DatabaseSecret, error) {
	return s.queries.ListDatabaseSecrets(ctx, databaseID)
}
