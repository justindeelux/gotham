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

type Repository interface {
	CreateProject(ctx context.Context, teamID uuid.UUID, name, description string) (Project, Environment, error)
	ListProjects(ctx context.Context, teamID uuid.UUID) ([]Project, error)
	GetProject(ctx context.Context, teamID, projectID uuid.UUID) (Project, error)
	UpdateProject(ctx context.Context, teamID, projectID uuid.UUID, name, description string) (Project, error)
	DeleteProject(ctx context.Context, teamID, projectID uuid.UUID) (orphanVolumes []string, err error)
	CreateEnvironment(ctx context.Context, teamID, projectID uuid.UUID, name string) (Environment, error)
	ListEnvironments(ctx context.Context, teamID, projectID uuid.UUID) ([]Environment, error)
	GetEnvironment(ctx context.Context, teamID, environmentID uuid.UUID) (Environment, error)
	UpdateEnvironment(ctx context.Context, teamID, environmentID uuid.UUID, name string) (Environment, error)
	DeleteEnvironmentIfNotLast(ctx context.Context, teamID, environmentID uuid.UUID) (orphanVolumes []string, err error)
	CountEnvironments(ctx context.Context, projectID uuid.UUID) (int, error)
	ListVariables(ctx context.Context, projectID, environmentID uuid.UUID) ([]SharedVariable, error)
	ReplaceVariables(ctx context.Context, projectID, environmentID uuid.UUID, build func(existing []SharedVariable) ([]SharedVariable, error)) error
	PreviewBaseIDs(ctx context.Context, teamID uuid.UUID, previewAppIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
}

type storeRepository struct {
	store *store.Store
}

var _ Repository = (*storeRepository)(nil)

func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

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
		if isForeignKeyViolation(err) {
			return Environment{}, ErrNotFound
		}
		return Environment{}, fmt.Errorf("projects: create environment: %w", err)
	}
	return environmentFromRow(row), nil
}

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

func (r *storeRepository) CountEnvironments(ctx context.Context, projectID uuid.UUID) (int, error) {
	count, err := r.store.CountEnvironmentsByProject(ctx, pgUUID(projectID))
	if err != nil {
		return 0, fmt.Errorf("projects: count environments: %w", err)
	}
	return int(count), nil
}

func (r *storeRepository) PreviewBaseIDs(ctx context.Context, teamID uuid.UUID, previewAppIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	bases := make(map[uuid.UUID]uuid.UUID, len(previewAppIDs))
	if len(previewAppIDs) == 0 {
		return bases, nil
	}
	ids := make([]pgtype.UUID, 0, len(previewAppIDs))
	for _, id := range previewAppIDs {
		ids = append(ids, pgUUID(id))
	}
	rows, err := r.store.ListPreviewBases(ctx, sqlc.ListPreviewBasesParams{
		PreviewAppIds: ids,
		TeamID:        pgUUID(teamID),
	})
	if err != nil {
		return nil, fmt.Errorf("projects: list preview bases: %w", err)
	}
	for _, row := range rows {
		bases[uuidFromPG(row.PreviewApplicationID)] = uuidFromPG(row.ApplicationID)
	}
	return bases, nil
}

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

func environmentFromRow(row sqlc.Environment) Environment {
	return Environment{
		ID:        uuidFromPG(row.ID),
		ProjectID: uuidFromPG(row.ProjectID),
		Name:      row.Name,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
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

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}
