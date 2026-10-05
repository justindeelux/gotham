package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Repository persists projects and environments. It is implemented over
// *store.Store (sqlc) in production and by fakes in tests.
type Repository interface {
	// CreateProject stores a project with its production environment in one
	// transaction. A duplicate name answers ErrProjectExists.
	CreateProject(ctx context.Context, teamID uuid.UUID, name, description string) (Project, Environment, error)
	// ListProjects returns a team's projects, newest first.
	ListProjects(ctx context.Context, teamID uuid.UUID) ([]Project, error)
	// GetProject returns one project of a team, or ErrNotFound.
	GetProject(ctx context.Context, teamID, projectID uuid.UUID) (Project, error)
	// UpdateProject applies a rename and/or description change. A duplicate
	// name answers ErrProjectExists; a foreign ID answers ErrNotFound.
	UpdateProject(ctx context.Context, teamID, projectID uuid.UUID, name, description string) (Project, error)
	// DeleteProject removes one project of a team (its environments
	// cascade), or ErrNotFound when no row matched. Soft-deleted services
	// and databases are purged in the same transaction; purged database
	// volumes may still exist on their nodes and come back as orphanVolumes.
	DeleteProject(ctx context.Context, teamID, projectID uuid.UUID) (orphanVolumes []string, err error)
	// CreateEnvironment stores an environment of a project. A foreign project
	// ID answers ErrNotFound — including a project deleted between the team
	// check and the INSERT (the FK refusal maps to 404, never 500); a
	// duplicate name answers ErrEnvironmentExists.
	CreateEnvironment(ctx context.Context, teamID, projectID uuid.UUID, name string) (Environment, error)
	// ListEnvironments returns a project's environments, oldest first, or
	// ErrNotFound when the project is foreign.
	ListEnvironments(ctx context.Context, teamID, projectID uuid.UUID) ([]Environment, error)
	// GetEnvironment returns one environment of a team, or ErrNotFound.
	GetEnvironment(ctx context.Context, teamID, environmentID uuid.UUID) (Environment, error)
	// UpdateEnvironment renames one environment of a team.
	UpdateEnvironment(ctx context.Context, teamID, environmentID uuid.UUID, name string) (Environment, error)
	// DeleteEnvironmentIfNotLast removes one environment of a team, or
	// ErrNotFound when no row matched. The survivor check and the delete run
	// in one transaction that locks the parent project row, so two
	// concurrent deletes of a project's last two environments cannot both
	// succeed. A refusal because the project would be left empty answers
	// ErrLastEnvironment. Soft-deleted services and databases are purged in
	// the same transaction; purged database volumes may still exist on
	// their nodes and come back as orphanVolumes.
	DeleteEnvironmentIfNotLast(ctx context.Context, teamID, environmentID uuid.UUID) (orphanVolumes []string, err error)
	// CountEnvironments reports how many environments a project holds.
	CountEnvironments(ctx context.Context, projectID uuid.UUID) (int, error)
	// ListVariables returns one scope's stored rows (environmentID Nil reads
	// the project level), ordered by key.
	ListVariables(ctx context.Context, projectID, environmentID uuid.UUID) ([]SharedVariable, error)
	// ReplaceVariables swaps one scope's whole set in a locked transaction
	// (see Store.ReplaceSharedVariablesLocked). build resolves the new set
	// from the existing rows inside that transaction, so keep-ciphertext
	// reads the latest committed set; a missing scope answers ErrNotFound.
	ReplaceVariables(ctx context.Context, projectID, environmentID uuid.UUID, build func(existing []SharedVariable) ([]SharedVariable, error)) error
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

// CreateProject implements Repository: the project and its production
// environment commit in one transaction, so a project never exists without
// an environment.
func (r *storeRepository) CreateProject(ctx context.Context, teamID uuid.UUID, name, description string) (Project, Environment, error) {
	row, envRow, err := r.store.CreateProjectWithEnvironment(ctx, sqlc.CreateProjectParams{
		ID:          pgUUID(uuid.New()),
		TeamID:      pgUUID(teamID),
		Name:        name,
		Description: description,
	}, pgUUID(uuid.New()), ProductionEnvironment)
	if err != nil {
		if isUniqueViolation(err) {
			return Project{}, Environment{}, ErrProjectExists
		}
		return Project{}, Environment{}, fmt.Errorf("projects: create project: %w", err)
	}
	return projectFromRow(row), environmentFromRow(envRow), nil
}

// ListProjects implements Repository.
func (r *storeRepository) ListProjects(ctx context.Context, teamID uuid.UUID) ([]Project, error) {
	rows, err := r.store.ListProjectsByTeam(ctx, pgUUID(teamID))
	if err != nil {
		return nil, fmt.Errorf("projects: list projects: %w", err)
	}
	projects := make([]Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, projectFromRow(row))
	}
	return projects, nil
}

// GetProject implements Repository, mapping a missing row to ErrNotFound.
func (r *storeRepository) GetProject(ctx context.Context, teamID, projectID uuid.UUID) (Project, error) {
	row, err := r.store.GetProject(ctx, sqlc.GetProjectParams{
		ID:     pgUUID(projectID),
		TeamID: pgUUID(teamID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, fmt.Errorf("projects: get project: %w", err)
	}
	return projectFromRow(row), nil
}

// UpdateProject implements Repository.
func (r *storeRepository) UpdateProject(ctx context.Context, teamID, projectID uuid.UUID, name, description string) (Project, error) {
	row, err := r.store.UpdateProject(ctx, sqlc.UpdateProjectParams{
		ID:          pgUUID(projectID),
		TeamID:      pgUUID(teamID),
		Name:        name,
		Description: description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return Project{}, ErrProjectExists
		}
		return Project{}, fmt.Errorf("projects: update project: %w", err)
	}
	return projectFromRow(row), nil
}

// DeleteProject implements Repository. The environments cascade; a
// foreign-key refusal (PE-2's RESTRICT from a racing resource insert)
// surfaces as ErrProjectNotEmpty, matching the service's pre-check.
func (r *storeRepository) DeleteProject(ctx context.Context, teamID, projectID uuid.UUID) ([]string, error) {
	affected, volumes, err := r.store.DeleteProject(ctx, sqlc.DeleteProjectParams{
		ID:     pgUUID(projectID),
		TeamID: pgUUID(teamID),
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, ErrProjectNotEmpty
		}
		return nil, fmt.Errorf("projects: delete project: %w", err)
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return volumes, nil
}

// CreateEnvironment implements Repository.
func (r *storeRepository) CreateEnvironment(ctx context.Context, teamID, projectID uuid.UUID, name string) (Environment, error) {
	if _, err := r.GetProject(ctx, teamID, projectID); err != nil {
		return Environment{}, err
	}
	row, err := r.store.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgUUID(uuid.New()),
		ProjectID: pgUUID(projectID),
		Name:      name,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Environment{}, ErrEnvironmentExists
		}
		// The project was deleted between the GetProject above and this
		// INSERT: a foreign project answers 404, never 500.
		if isForeignKeyViolation(err) {
			return Environment{}, ErrNotFound
		}
		return Environment{}, fmt.Errorf("projects: create environment: %w", err)
	}
	return environmentFromRow(row), nil
}

// ListEnvironments implements Repository.
func (r *storeRepository) ListEnvironments(ctx context.Context, teamID, projectID uuid.UUID) ([]Environment, error) {
	if _, err := r.GetProject(ctx, teamID, projectID); err != nil {
		return nil, err
	}
	rows, err := r.store.ListEnvironmentsByProject(ctx, pgUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("projects: list environments: %w", err)
	}
	environments := make([]Environment, 0, len(rows))
	for _, row := range rows {
		environments = append(environments, environmentFromRow(row))
	}
	return environments, nil
}

// GetEnvironment implements Repository, mapping a missing row to ErrNotFound.
func (r *storeRepository) GetEnvironment(ctx context.Context, teamID, environmentID uuid.UUID) (Environment, error) {
	row, err := r.store.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
		ID:     pgUUID(environmentID),
		TeamID: pgUUID(teamID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Environment{}, ErrNotFound
		}
		return Environment{}, fmt.Errorf("projects: get environment: %w", err)
	}
	return environmentFromRow(row), nil
}

// UpdateEnvironment implements Repository.
func (r *storeRepository) UpdateEnvironment(ctx context.Context, teamID, environmentID uuid.UUID, name string) (Environment, error) {
	row, err := r.store.UpdateEnvironment(ctx, sqlc.UpdateEnvironmentParams{
		ID:     pgUUID(environmentID),
		Name:   name,
		TeamID: pgUUID(teamID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Environment{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return Environment{}, ErrEnvironmentExists
		}
		return Environment{}, fmt.Errorf("projects: update environment: %w", err)
	}
	return environmentFromRow(row), nil
}

// DeleteEnvironmentIfNotLast implements Repository, with the same
// racing-insert mapping as DeleteProject.
func (r *storeRepository) DeleteEnvironmentIfNotLast(ctx context.Context, teamID, environmentID uuid.UUID) ([]string, error) {
	removed, volumes, err := r.store.DeleteEnvironmentGuarded(ctx, pgUUID(teamID), pgUUID(environmentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if isForeignKeyViolation(err) {
			return nil, ErrEnvironmentNotEmpty
		}
		return nil, fmt.Errorf("projects: delete environment: %w", err)
	}
	if !removed {
		return nil, ErrLastEnvironment
	}
	return volumes, nil
}

// CountEnvironments implements Repository.
func (r *storeRepository) CountEnvironments(ctx context.Context, projectID uuid.UUID) (int, error) {
	count, err := r.store.CountEnvironmentsByProject(ctx, pgUUID(projectID))
	if err != nil {
		return 0, fmt.Errorf("projects: count environments: %w", err)
	}
	return int(count), nil
}

// projectFromRow maps a sqlc row to the domain project.
func projectFromRow(row sqlc.Project) Project {
	return Project{
		ID:          uuidFromPG(row.ID),
		TeamID:      uuidFromPG(row.TeamID),
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

// environmentFromRow maps a sqlc row to the domain environment.
func environmentFromRow(row sqlc.Environment) Environment {
	return Environment{
		ID:        uuidFromPG(row.ID),
		ProjectID: uuidFromPG(row.ProjectID),
		Name:      row.Name,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), raised by the case-insensitive name indexes.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation reports whether err is a PostgreSQL foreign-key
// violation (SQLSTATE 23503), raised by PE-2's RESTRICT constraints when a
// project or environment is deleted while a racing resource insert committed.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// pgUUID converts a domain UUID for sqlc; the zero UUID becomes NULL.
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
