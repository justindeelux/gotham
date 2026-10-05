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
	// UpdateDatabaseName persists only the name, or ErrNotFound when the row
	// is gone or already deleted. Scoping the write to one column keeps a
	// rename from clobbering a concurrent status or container-id change.
	UpdateDatabaseName(ctx context.Context, databaseID uuid.UUID, name string) (Database, error)
	// UpdateDatabaseTarget renames, moves environment and changes node in one
	// live-row-fenced write (or ErrNotFound when the row is gone or
	// deleted). Name collisions in the target environment surface as
	// ErrConflict; a foreign environment as ErrNotFound.
	UpdateDatabaseTarget(ctx context.Context, database Database) (Database, error)
	// ListDatabasesByEnvironment returns one environment's live databases,
	// newest first.
	ListDatabasesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Database, error)
	// ListDatabasesByProject returns every environment's live databases of
	// one project, newest first.
	ListDatabasesByProject(ctx context.Context, projectID uuid.UUID) ([]Database, error)
	// NameInEnvironment reports whether the environment holds another live
	// database with name (the move-collision pre-check).
	NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error)
	// ResolveEnvironment validates that environmentID belongs to teamID and
	// returns it (ErrNotFound for a foreign or missing one).
	ResolveEnvironment(ctx context.Context, environmentID, teamID uuid.UUID) (EnvironmentRef, error)
	// ResolveProject validates that projectID belongs to teamID
	// (ErrNotFound for a foreign or missing one).
	ResolveProject(ctx context.Context, projectID, teamID uuid.UUID) (uuid.UUID, error)
	// UpdateDatabaseContainer persists only the container id, or ErrNotFound
	// when the row is gone or already deleted (the write must not resurrect a
	// soft-deleted row).
	UpdateDatabaseContainer(ctx context.Context, databaseID uuid.UUID, containerID string) (Database, error)
	// UpdateDatabaseStatus persists only the status, or ErrNotFound when the
	// row is gone or already deleted.
	UpdateDatabaseStatus(ctx context.Context, databaseID uuid.UUID, status Status) (Database, error)
	// PublicPortInUse reports whether a live database already publishes
	// publicPort on serverID.
	PublicPortInUse(ctx context.Context, serverID uuid.UUID, publicPort int32) (bool, error)
	// SoftDeleteDatabase marks the row deleted without touching the volume.
	SoftDeleteDatabase(ctx context.Context, databaseID uuid.UUID) (Database, error)
	// ListExpiredDatabases returns soft-deleted databases whose grace window
	// ended at or before cutoff, oldest deletion first. It is the retention
	// sweep's selection.
	ListExpiredDatabases(ctx context.Context, cutoff time.Time) ([]Database, error)
	// PurgeDatabase hard-deletes a soft-deleted database and its sealed
	// credentials. A live row is never purged.
	//
	// The delete cascades (ON DELETE CASCADE) to the database's backups and
	// backup_schedules rows. It does NOT remove the stored backup artifacts
	// (local files, node volumes, S3 objects); a soft-deleted database's
	// backups are already invisible to every read, so those artifacts are
	// orphaned here rather than reapable. Reaping them is tracked for FX-9
	// (backup durability); see RetentionSweeper.
	PurgeDatabase(ctx context.Context, databaseID uuid.UUID) error
	// CreateSecret stores one sealed credential of a database.
	CreateSecret(ctx context.Context, secret Secret) (Secret, error)
	// ListSecrets returns a database's sealed credentials, sorted by key.
	ListSecrets(ctx context.Context, databaseID uuid.UUID) ([]Secret, error)
	// DeleteDatabaseSecrets removes every sealed credential of a database,
	// rolling back a partially-written credential set on create failure.
	DeleteDatabaseSecrets(ctx context.Context, databaseID uuid.UUID) error
	// ServerExists reports whether the target node is registered AND
	// actionable by the caller's active team. A node of another team answers
	// false, like a missing one, so node IDs cannot be probed (F6); a legacy
	// node without a team stays shared.
	ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error)
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
		ID:            pgUUID(database.ID),
		UserID:        pgUUID(database.UserID),
		TeamID:        pgUUID(database.TeamID),
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

// GetDatabase implements Repository, mapping a missing row to ErrNotFound.
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
	return r.enrich(ctx, rows)
}

// ListDatabasesByEnvironment implements Repository.
func (r *storeRepository) ListDatabasesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Database, error) {
	rows, err := r.store.ListDatabasesByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return nil, fmt.Errorf("databases: list databases by environment: %w", err)
	}
	return r.enrich(ctx, rows)
}

// ListDatabasesByProject implements Repository.
func (r *storeRepository) ListDatabasesByProject(ctx context.Context, projectID uuid.UUID) ([]Database, error) {
	rows, err := r.store.ListDatabasesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("databases: list databases by project: %w", err)
	}
	return r.enrich(ctx, rows)
}

// NameInEnvironment implements Repository: the move-collision pre-check. The
// comparison is exact, matching the (environment_id, name) unique index.
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

// ResolveEnvironment implements Repository: a foreign or missing environment
// answers ErrNotFound, so environment IDs cannot be probed across teams.
func (r *storeRepository) ResolveEnvironment(ctx context.Context, environmentID, teamID uuid.UUID) (EnvironmentRef, error) {
	row, err := r.store.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
		ID:     pgUUID(environmentID),
		TeamID: pgUUID(teamID),
	})
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

// ResolveProject implements Repository.
func (r *storeRepository) ResolveProject(ctx context.Context, projectID, teamID uuid.UUID) (uuid.UUID, error) {
	if _, err := r.store.GetProject(ctx, sqlc.GetProjectParams{
		ID:     pgUUID(projectID),
		TeamID: pgUUID(teamID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("databases: get project: %w", err)
	}
	return projectID, nil
}

// enrich maps rows to the domain model and fills the environment, project
// and server names from the contract. Rows of one listing share environments,
// so each distinct parent is read once.
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
				fetched, err := r.store.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
					ID:     pgUUID(database.EnvironmentID),
					TeamID: pgUUID(database.TeamID),
				})
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
					fetched, err := r.store.GetProject(ctx, sqlc.GetProjectParams{
						ID:     pgUUID(database.ProjectID),
						TeamID: pgUUID(database.TeamID),
					})
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

// UpdateDatabaseName implements Repository, mapping a missing (or already
// soft-deleted) row to ErrNotFound.
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

// UpdateDatabaseTarget implements Repository: rename, move environment and
// change node in one live-row-fenced write.
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

// UpdateDatabaseContainer implements Repository.
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

// UpdateDatabaseStatus implements Repository.
func (r *storeRepository) UpdateDatabaseStatus(ctx context.Context, databaseID uuid.UUID, status Status) (Database, error) {
	row, err := r.store.UpdateDatabaseStatus(ctx, sqlc.UpdateDatabaseStatusParams{
		ID:     pgUUID(databaseID),
		Status: string(status),
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

// PublicPortInUse implements Repository.
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

// updateDatabaseError maps the shared failure modes of the column-scoped
// updates: a unique-name violation is a conflict and a missing/fenced row is
// not-found.
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
	enriched, err := r.enrich(ctx, []sqlc.Database{row})
	if err != nil {
		return Database{}, err
	}
	return enriched[0], nil
}

// ListExpiredDatabases implements Repository: the soft-deleted rows the
// retention sweep must purge, oldest deletion first.
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

// PurgeDatabase implements Repository. Purge is idempotent: a row that is
// already gone (or is live) removes zero rows and is not an error, because the
// sweep only needs the row to be absent.
func (r *storeRepository) PurgeDatabase(ctx context.Context, databaseID uuid.UUID) error {
	if _, err := r.store.PurgeDatabase(ctx, pgUUID(databaseID)); err != nil {
		return fmt.Errorf("databases: purge database: %w", err)
	}
	return nil
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

// DeleteDatabaseSecrets implements Repository.
func (r *storeRepository) DeleteDatabaseSecrets(ctx context.Context, databaseID uuid.UUID) error {
	if err := r.store.DeleteDatabaseSecrets(ctx, pgUUID(databaseID)); err != nil {
		return fmt.Errorf("databases: delete secrets: %w", err)
	}
	return nil
}

// ServerExists implements Repository using the node registry table and the
// caller's active team.
func (r *storeRepository) ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error) {
	if serverID == uuid.Nil {
		return false, nil
	}
	row, err := r.store.GetServerByID(ctx, pgUUID(serverID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("databases: resolve server: %w", err)
	}
	if err := scope.AuthorizeOptionalTeam(uuidFromPG(row.TeamID), true); err != nil {
		return false, nil
	}
	return true, nil
}

// databaseFromRow maps one sqlc row onto the domain type.
func databaseFromRow(row sqlc.Database) Database {
	return Database{
		ID:            uuidFromPG(row.ID),
		UserID:        uuidFromPG(row.UserID),
		TeamID:        uuidFromPG(row.TeamID),
		ServerID:      uuidFromPG(row.ServerID),
		EnvironmentID: uuidFromPG(row.EnvironmentID),
		Name:          row.Name,
		Engine:        row.Engine,
		Version:       row.Version,
		Status:        Status(row.Status),
		ContainerID:   row.ContainerID,
		PublicPort:    row.PublicPort,
		StoragePath:   row.StoragePath,
		CreatedAt:     timeFromPG(row.CreatedAt),
		UpdatedAt:     timeFromPG(row.UpdatedAt),
		DeletedAt:     timeFromPG(row.DeletedAt),
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

// isForeignKeyViolation reports a PostgreSQL foreign-key violation (SQLSTATE
// 23503): an environment deleted between validation and write.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// pgUUID converts a domain UUID for sqlc. The zero UUID becomes an invalid
// pgtype value, which serialises as NULL for nullable columns.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// pgTimestamp converts a domain time to a sqlc timestamptz column.
func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
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
