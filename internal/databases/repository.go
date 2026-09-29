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
	"github.com/justindeelux/gotham/internal/teams"
)

// Repository persists databases and their sealed credentials. It is
// implemented over *store.Store (sqlc) in production and by fakes in tests.
// Soft-deleted rows are never returned: the queries filter deleted_at, so a
// deleted database disappears from every read until it is purged.
type Repository interface {
	// CreateDatabase stores a new database row, or ErrConflict when a live
	// database of the same user and name exists.
	CreateDatabase(ctx context.Context, database Database) (Database, error)
	// GetDatabase returns a live database, or ErrNotFound.
	GetDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error)
	// ListDatabases returns the live databases of the scope's active team
	// (or, without a team context, of the creator), newest first.
	ListDatabases(ctx context.Context, scope teams.Scope) ([]Database, error)
	// UpdateDatabase persists the mutable fields, or ErrNotFound when the row
	// is gone or already deleted.
	UpdateDatabase(ctx context.Context, database Database) (Database, error)
	// SoftDeleteDatabase marks the row deleted without touching the volume.
	SoftDeleteDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error)
	// CreateSecret stores one sealed credential of a database.
	CreateSecret(ctx context.Context, secret Secret) (Secret, error)
	// ListSecrets returns a database's sealed credentials, sorted by key.
	ListSecrets(ctx context.Context, databaseID uuid.UUID) ([]Secret, error)
	// ServerExists reports whether the target node is registered. Servers are
	// a shared resource in this schema, so there is no per-user check to make.
	ServerExists(ctx context.Context, serverID uuid.UUID) (bool, error)
}

// storeRepository adapts *store.Store to Repository.
type storeRepository struct {
	store *store.Store
}

// Compile-time guarantee.
var _ Repository = (*storeRepository)(nil)

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

// CreateDatabase implements Repository. The partial unique index on live names
// turns a concurrent create into ErrConflict.
func (r *storeRepository) CreateDatabase(ctx context.Context, database Database) (Database, error) {
	row, err := r.store.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID:          pgUUID(database.ID),
		UserID:      pgUUID(database.UserID),
		TeamID:      pgUUID(database.TeamID),
		ServerID:    pgUUID(database.ServerID),
		Name:        database.Name,
		Engine:      database.Engine,
		Version:     database.Version,
		Status:      string(database.Status),
		PublicPort:  database.PublicPort,
		StoragePath: database.StoragePath,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Database{}, ErrConflict
		}
		return Database{}, fmt.Errorf("databases: create database: %w", err)
	}
	return databaseFromRow(row), nil
}

// GetDatabase implements Repository, mapping a missing row to ErrNotFound.
func (r *storeRepository) GetDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error) {
	row, err := r.store.GetDatabase(ctx, pgUUID(databaseID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Database{}, ErrNotFound
		}
		return Database{}, fmt.Errorf("databases: get database: %w", err)
	}
	return databaseFromRow(row), nil
}

// ListDatabases implements Repository: the active team's live databases, or
// the creator's when no team context is present.
func (r *storeRepository) ListDatabases(ctx context.Context, scope teams.Scope) ([]Database, error) {
	var (
		rows []sqlc.Database
		err  error
	)
	if scope.Active() {
		rows, err = r.store.ListDatabasesByTeam(ctx, pgUUID(scope.TeamID))
	} else {
		rows, err = r.store.ListDatabasesByUser(ctx, pgUUID(scope.UserID))
	}
	if err != nil {
		return nil, fmt.Errorf("databases: list databases: %w", err)
	}
	databases := make([]Database, 0, len(rows))
	for _, row := range rows {
		databases = append(databases, databaseFromRow(row))
	}
	return databases, nil
}

// UpdateDatabase implements Repository, mapping a missing row to ErrNotFound.
func (r *storeRepository) UpdateDatabase(ctx context.Context, database Database) (Database, error) {
	row, err := r.store.UpdateDatabase(ctx, sqlc.UpdateDatabaseParams{
		ID:          pgUUID(database.ID),
		Name:        database.Name,
		Status:      string(database.Status),
		ContainerID: database.ContainerID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Database{}, ErrConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Database{}, ErrNotFound
		}
		return Database{}, fmt.Errorf("databases: update database: %w", err)
	}
	return databaseFromRow(row), nil
}

// SoftDeleteDatabase implements Repository. A row that is already deleted
// reports ErrNotFound, which makes the delete idempotent for its caller.
func (r *storeRepository) SoftDeleteDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error) {
	row, err := r.store.SoftDeleteDatabase(ctx, pgUUID(databaseID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Database{}, ErrNotFound
		}
		return Database{}, fmt.Errorf("databases: soft delete database: %w", err)
	}
	return databaseFromRow(row), nil
}

// CreateSecret implements Repository.
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

// ListSecrets implements Repository.
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

// ServerExists implements Repository using the node registry table.
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

// databaseFromRow maps one sqlc row onto the domain type.
func databaseFromRow(row sqlc.Database) Database {
	return Database{
		ID:          uuidFromPG(row.ID),
		UserID:      uuidFromPG(row.UserID),
		TeamID:      uuidFromPG(row.TeamID),
		ServerID:    uuidFromPG(row.ServerID),
		Name:        row.Name,
		Engine:      row.Engine,
		Version:     row.Version,
		Status:      Status(row.Status),
		ContainerID: row.ContainerID,
		PublicPort:  row.PublicPort,
		StoragePath: row.StoragePath,
		CreatedAt:   timeFromPG(row.CreatedAt),
		UpdatedAt:   timeFromPG(row.UpdatedAt),
		DeletedAt:   timeFromPG(row.DeletedAt),
	}
}

// secretFromRow maps one sqlc database_secrets row onto the domain type.
func secretFromRow(row sqlc.DatabaseSecret) Secret {
	return Secret{
		ID:         uuidFromPG(row.ID),
		DatabaseID: uuidFromPG(row.DatabaseID),
		Key:        row.Key,
		Ciphertext: row.Ciphertext,
		CreatedAt:  timeFromPG(row.CreatedAt),
	}
}

// The pg helpers below mirror the deploy package's: they are unexported there,
// and domain packages do not import each other just to share four conversions.

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), raised by databases_user_name_idx.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// pgUUID converts a domain UUID for sqlc. The zero UUID becomes an invalid
// pgtype value, which serialises as NULL for nullable columns.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a sqlc UUID column to the domain type (NULL → zero).
func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

// timeFromPG converts a sqlc timestamp column to the domain type.
func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}
