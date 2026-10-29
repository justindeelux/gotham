package databases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Repository interface {
	CreateDatabase(ctx context.Context, database Database) (Database, error)
	GetDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error)
	ListDatabases(ctx context.Context, userID uuid.UUID) ([]Database, error)
	UpdateDatabaseName(ctx context.Context, databaseID uuid.UUID, name string) (Database, error)
	UpdateDatabaseTarget(ctx context.Context, database Database) (Database, error)
	ListDatabasesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Database, error)
	ListDatabasesByProject(ctx context.Context, projectID uuid.UUID) ([]Database, error)
	NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error)
	ResolveEnvironment(ctx context.Context, environmentID uuid.UUID) (EnvironmentRef, error)
	ResolveProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error)
	UpdateDatabaseContainer(ctx context.Context, databaseID uuid.UUID, containerID string) (Database, error)
	UpdateDatabaseStatus(ctx context.Context, databaseID uuid.UUID, status Status, message string) (Database, error)
	ListDatabasesByStatus(ctx context.Context, status Status) ([]Database, error)
	PublicPortInUse(ctx context.Context, serverID uuid.UUID, publicPort int32) (bool, error)
	SoftDeleteDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error)
	ListExpiredDatabases(ctx context.Context, cutoff time.Time) ([]Database, error)
	PurgeDatabase(ctx context.Context, databaseID uuid.UUID) error
	CreateSecret(ctx context.Context, secret Secret) (Secret, error)
	ListSecrets(ctx context.Context, databaseID uuid.UUID) ([]Secret, error)
	DeleteDatabaseSecrets(ctx context.Context, databaseID uuid.UUID) error
	ServerExists(ctx context.Context, serverID uuid.UUID) (bool, error)
}

type storeRepository struct {
	store *store.Store
}

var _ Repository = (*storeRepository)(nil)

func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

func (r *storeRepository) CreateDatabase(ctx context.Context, database Database) (Database, error) {
	row, err := r.store.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID:            pgUUID(database.ID),
		UserID:        pgUUID(database.UserID),
		ServerID:      pgUUID(database.ServerID),
		EnvironmentID: pgUUID(database.EnvironmentID),
		Name:          database.Name,
		Engine:        database.Engine,
		Version:       database.Version,
		Status:        string(database.Status),
		PublicPort:    database.PublicPort,
		StoragePath:   database.StoragePath,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Database{}, ErrConflict
		}
		if isForeignKeyViolation(err) {
			return Database{}, ErrNotFound
		}
		return Database{}, fmt.Errorf("databases: create database: %w", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) GetDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error) {
	row, err := r.store.GetDatabase(ctx, pgUUID(databaseID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Database{}, ErrNotFound
		}
		return Database{}, fmt.Errorf("databases: get database: %w", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) ListDatabases(ctx context.Context, userID uuid.UUID) ([]Database, error) {
	rows, err := r.store.ListDatabasesByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("databases: list databases: %w", err)
	}
	return r.enrich(ctx, rows)
}

func (r *storeRepository) ListDatabasesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Database, error) {
	rows, err := r.store.ListDatabasesByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return nil, fmt.Errorf("databases: list databases by environment: %w", err)
	}
	return r.enrich(ctx, rows)
}

func (r *storeRepository) ListDatabasesByProject(ctx context.Context, projectID uuid.UUID) ([]Database, error) {
	rows, err := r.store.ListDatabasesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("databases: list databases by project: %w", err)
	}
	return r.enrich(ctx, rows)
}

func (r *storeRepository) ListDatabasesByStatus(ctx context.Context, status Status) ([]Database, error) {
	rows, err := r.store.ListDatabasesByStatus(ctx, string(status))
	if err != nil {
		return nil, fmt.Errorf("databases: list databases by status: %w", err)
	}
	return r.enrich(ctx, rows)
}

func (r *storeRepository) NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error) {
	collision, err := r.store.DatabaseNameInEnvironment(ctx, sqlc.DatabaseNameInEnvironmentParams{
		EnvironmentID: pgUUID(environmentID),
		Name:          name,
		ID:            pgUUID(exceptID),
	})
	if err != nil {
		return false, fmt.Errorf("databases: name in environment: %w", err)
	}
	return collision, nil
}

func (r *storeRepository) ResolveEnvironment(ctx context.Context, environmentID uuid.UUID) (EnvironmentRef, error) {
	row, err := r.store.GetEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EnvironmentRef{}, ErrNotFound
		}
		return EnvironmentRef{}, fmt.Errorf("databases: get environment: %w", err)
	}
	return EnvironmentRef{
		ID:        uuidFromPG(row.ID),
		ProjectID: uuidFromPG(row.ProjectID),
		Name:      row.Name,
	}, nil
}

func (r *storeRepository) ResolveProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	if _, err := r.store.GetProject(ctx, pgUUID(projectID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("databases: get project: %w", err)
	}
	return projectID, nil
}

func (r *storeRepository) enrich(ctx context.Context, rows []sqlc.Database) ([]Database, error) {
	databases := make([]Database, 0, len(rows))
	envs := make(map[uuid.UUID]sqlc.Environment)
	projects := make(map[uuid.UUID]sqlc.Project)
	servers := make(map[uuid.UUID]string)
	for _, row := range rows {
		database := databaseFromRow(row)
		if database.EnvironmentID != uuid.Nil {
			env, ok := envs[database.EnvironmentID]
			if !ok {
				fetched, err := r.store.GetEnvironment(ctx, pgUUID(database.EnvironmentID))
				if err != nil {
					return nil, fmt.Errorf("databases: get environment: %w", err)
				}
				env, envs[database.EnvironmentID] = fetched, fetched
			}
			database.EnvironmentName = env.Name
			database.ProjectID = uuidFromPG(env.ProjectID)
			if database.ProjectID != uuid.Nil {
				project, ok := projects[database.ProjectID]
				if !ok {
					fetched, err := r.store.GetProject(ctx, pgUUID(database.ProjectID))
					if err != nil {
						return nil, fmt.Errorf("databases: get project: %w", err)
					}
					project, projects[database.ProjectID] = fetched, fetched
				}
				database.ProjectName = project.Name
			}
		}
		if database.ServerID != uuid.Nil {
			name, ok := servers[database.ServerID]
			if !ok {
				server, err := r.store.GetServerByID(ctx, pgUUID(database.ServerID))
				if err != nil {
					return nil, fmt.Errorf("databases: get server: %w", err)
				}
				name, servers[database.ServerID] = server.Name, server.Name
			}
			database.ServerName = name
		}
		databases = append(databases, database)
	}
	return databases, nil
}

func (r *storeRepository) UpdateDatabaseName(ctx context.Context, databaseID uuid.UUID, name string) (Database, error) {
	row, err := r.store.UpdateDatabaseName(ctx, sqlc.UpdateDatabaseNameParams{
		ID:   pgUUID(databaseID),
		Name: name,
	})
	if err != nil {
		return Database{}, updateDatabaseError("name", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) UpdateDatabaseTarget(ctx context.Context, database Database) (Database, error) {
	row, err := r.store.UpdateDatabaseTarget(ctx, sqlc.UpdateDatabaseTargetParams{
		ID:            pgUUID(database.ID),
		Name:          database.Name,
		EnvironmentID: pgUUID(database.EnvironmentID),
		ServerID:      pgUUID(database.ServerID),
	})
	if err != nil {
		return Database{}, updateDatabaseError("target", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) UpdateDatabaseContainer(ctx context.Context, databaseID uuid.UUID, containerID string) (Database, error) {
	row, err := r.store.UpdateDatabaseContainer(ctx, sqlc.UpdateDatabaseContainerParams{
		ID:          pgUUID(databaseID),
		ContainerID: containerID,
	})
	if err != nil {
		return Database{}, updateDatabaseError("container", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) UpdateDatabaseStatus(ctx context.Context, databaseID uuid.UUID, status Status, message string) (Database, error) {
	row, err := r.store.UpdateDatabaseStatus(ctx, sqlc.UpdateDatabaseStatusParams{
		ID:            pgUUID(databaseID),
		Status:        string(status),
		StatusMessage: pgOptionalText(message),
	})
	if err != nil {
		return Database{}, updateDatabaseError("status", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) PublicPortInUse(ctx context.Context, serverID uuid.UUID, publicPort int32) (bool, error) {
	inUse, err := r.store.PublicPortInUse(ctx, sqlc.PublicPortInUseParams{
		ServerID:   pgUUID(serverID),
		PublicPort: publicPort,
	})
	if err != nil {
		return false, fmt.Errorf("databases: check public port: %w", err)
	}
	return inUse, nil
}

func updateDatabaseError(column string, err error) error {
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("databases: update database %s: %w", column, err)
}

func (r *storeRepository) SoftDeleteDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error) {
	row, err := r.store.SoftDeleteDatabase(ctx, pgUUID(databaseID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Database{}, ErrNotFound
		}
		return Database{}, fmt.Errorf("databases: soft delete database: %w", err)
	}
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) ListExpiredDatabases(ctx context.Context, cutoff time.Time) ([]Database, error) {
	rows, err := r.store.ListExpiredDatabases(ctx, pgTimestamp(cutoff))
	if err != nil {
		return nil, fmt.Errorf("databases: list expired databases: %w", err)
	}
	databases := make([]Database, 0, len(rows))
	for _, row := range rows {
		databases = append(databases, databaseFromRow(row))
	}
	return databases, nil
}

func (r *storeRepository) PurgeDatabase(ctx context.Context, databaseID uuid.UUID) error {
	if _, err := r.store.PurgeDatabase(ctx, pgUUID(databaseID)); err != nil {
		return fmt.Errorf("databases: purge database: %w", err)
	}
	return nil
}

func (r *storeRepository) CreateSecret(ctx context.Context, secret Secret) (Secret, error) {
	row, err := r.store.CreateDatabaseSecret(ctx, sqlc.CreateDatabaseSecretParams{
		DatabaseID: pgUUID(secret.DatabaseID),
		Key:        secret.Key,
		Ciphertext: secret.Ciphertext,
	})
	if err != nil {
		return Secret{}, fmt.Errorf("databases: create secret: %w", err)
	}
	return secretFromRow(row), nil
}

func (r *storeRepository) ListSecrets(ctx context.Context, databaseID uuid.UUID) ([]Secret, error) {
	rows, err := r.store.ListDatabaseSecrets(ctx, pgUUID(databaseID))
	if err != nil {
		return nil, fmt.Errorf("databases: list secrets: %w", err)
	}
	secrets := make([]Secret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, secretFromRow(row))
	}
	return secrets, nil
}

func (r *storeRepository) DeleteDatabaseSecrets(ctx context.Context, databaseID uuid.UUID) error {
	if err := r.store.DeleteDatabaseSecrets(ctx, pgUUID(databaseID)); err != nil {
		return fmt.Errorf("databases: delete secrets: %w", err)
	}
	return nil
}

func (r *storeRepository) ServerExists(ctx context.Context, serverID uuid.UUID) (bool, error) {
	if serverID == uuid.Nil {
		return false, nil
	}
	if _, err := r.store.GetServerByID(ctx, pgUUID(serverID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("databases: resolve server: %w", err)
	}
	return true, nil
}

func databaseFromRow(row sqlc.Database) Database {
	return Database{
		ID:            uuidFromPG(row.ID),
		UserID:        uuidFromPG(row.UserID),
		ServerID:      uuidFromPG(row.ServerID),
		EnvironmentID: uuidFromPG(row.EnvironmentID),
		Name:          row.Name,
		Engine:        row.Engine,
		Version:       row.Version,
		Status:        Status(row.Status),
		StatusMessage: optionalText(row.StatusMessage),
		ContainerID:   row.ContainerID,
		PublicPort:    row.PublicPort,
		StoragePath:   row.StoragePath,
		CreatedAt:     timeFromPG(row.CreatedAt),
		UpdatedAt:     timeFromPG(row.UpdatedAt),
		DeletedAt:     timeFromPG(row.DeletedAt),
	}
}

func secretFromRow(row sqlc.DatabaseSecret) Secret {
	return Secret{
		ID:         uuidFromPG(row.ID),
		DatabaseID: uuidFromPG(row.DatabaseID),
		Key:        row.Key,
		Ciphertext: row.Ciphertext,
		CreatedAt:  timeFromPG(row.CreatedAt),
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgOptionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}
