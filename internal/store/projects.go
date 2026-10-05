package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateProjectWithEnvironment stores a project and its first environment
// (production) in one transaction, so a project can never exist without an
// environment.
func (s *Store) CreateProjectWithEnvironment(ctx context.Context, project sqlc.CreateProjectParams, environmentID pgtype.UUID, environmentName string) (sqlc.Project, sqlc.Environment, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Project{}, sqlc.Environment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	created, err := queries.CreateProject(ctx, project)
	if err != nil {
		return sqlc.Project{}, sqlc.Environment{}, err
	}
	environment, err := queries.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        environmentID,
		ProjectID: created.ID,
		Name:      environmentName,
	})
	if err != nil {
		return sqlc.Project{}, sqlc.Environment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Project{}, sqlc.Environment{}, err
	}
	return created, environment, nil
}

// CreateProject stores a project row and returns it.
func (s *Store) CreateProject(ctx context.Context, params sqlc.CreateProjectParams) (sqlc.Project, error) {
	return s.queries.CreateProject(ctx, params)
}

// GetProject returns one project of a team, or pgx.ErrNoRows when the ID is
// unknown or belongs to another team.
func (s *Store) GetProject(ctx context.Context, params sqlc.GetProjectParams) (sqlc.Project, error) {
	return s.queries.GetProject(ctx, params)
}

// ListProjectsByTeam returns a team's projects, newest first.
func (s *Store) ListProjectsByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Project, error) {
	return s.queries.ListProjectsByTeam(ctx, teamID)
}

// UpdateProject applies a project edit and returns the stored row.
func (s *Store) UpdateProject(ctx context.Context, params sqlc.UpdateProjectParams) (sqlc.Project, error) {
	return s.queries.UpdateProject(ctx, params)
}

// DeleteProject removes one project of a team and reports how many rows were
// removed (0 when the ID is unknown or foreign). Environments cascade.
func (s *Store) DeleteProject(ctx context.Context, params sqlc.DeleteProjectParams) (int64, error) {
	return s.queries.DeleteProject(ctx, params)
}

// CreateEnvironment stores an environment row and returns it.
func (s *Store) CreateEnvironment(ctx context.Context, params sqlc.CreateEnvironmentParams) (sqlc.Environment, error) {
	return s.queries.CreateEnvironment(ctx, params)
}

// GetEnvironment returns one environment of a team, or pgx.ErrNoRows when the
// ID is unknown or belongs to another team.
func (s *Store) GetEnvironment(ctx context.Context, params sqlc.GetEnvironmentParams) (sqlc.Environment, error) {
	return s.queries.GetEnvironment(ctx, params)
}

// ListEnvironmentsByProject returns a project's environments, oldest first.
// The caller checks the project belongs to the active team first, so a
// foreign project ID answers 404 before this runs.
func (s *Store) ListEnvironmentsByProject(ctx context.Context, projectID pgtype.UUID) ([]sqlc.Environment, error) {
	return s.queries.ListEnvironmentsByProject(ctx, projectID)
}

// UpdateEnvironment renames one environment of a team and returns the row.
func (s *Store) UpdateEnvironment(ctx context.Context, params sqlc.UpdateEnvironmentParams) (sqlc.Environment, error) {
	return s.queries.UpdateEnvironment(ctx, params)
}

// DeleteEnvironment removes one environment of a team and reports how many
// rows were removed (0 when the ID is unknown or foreign).
func (s *Store) DeleteEnvironment(ctx context.Context, params sqlc.DeleteEnvironmentParams) (int64, error) {
	return s.queries.DeleteEnvironment(ctx, params)
}

// CountEnvironmentsByProject reports how many environments a project holds.
// Deleting the last one is refused, so a project always has at least one.
func (s *Store) CountEnvironmentsByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountEnvironmentsByProject(ctx, projectID)
}
