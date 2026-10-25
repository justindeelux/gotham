package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateProjectWithEnvironment(ctx context.Context, project sqlc.CreateProjectParams, environmentID pgtype.UUID, environmentName string) (sqlc.Project, sqlc.Environment, error) {
	if requestreplay.From(ctx) != nil {
		return s.createProjectReplay(ctx, project, environmentID, environmentName)
	}
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

func (s *Store) CreateProject(ctx context.Context, params sqlc.CreateProjectParams) (sqlc.Project, error) {
	return s.queries.CreateProject(ctx, params)
}

func (s *Store) GetProject(ctx context.Context, id pgtype.UUID) (sqlc.Project, error) {
	return s.queries.GetProject(ctx, id)
}

func (s *Store) ListProjects(ctx context.Context) ([]sqlc.Project, error) {
	return s.queries.ListProjects(ctx)
}

func (s *Store) UpdateProject(ctx context.Context, params sqlc.UpdateProjectParams) (sqlc.Project, error) {
	return s.queries.UpdateProject(ctx, params)
}

func (s *Store) DeleteProject(ctx context.Context, id pgtype.UUID) (int64, []string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1)`,
		id).Scan(&exists); err != nil {
		return 0, nil, err
	}
	if !exists {
		return 0, nil, nil
	}
	if _, err := queries.PurgeTombstonedServicesByProject(ctx, id); err != nil {
		return 0, nil, err
	}
	volumes, err := queries.PurgeTombstonedDatabasesByProject(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	affected, err := queries.DeleteProject(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return affected, volumes, nil
}

func (s *Store) CreateEnvironment(ctx context.Context, params sqlc.CreateEnvironmentParams) (sqlc.Environment, error) {
	if requestreplay.From(ctx) != nil {
		return s.createEnvironmentReplay(ctx, params)
	}
	return s.queries.CreateEnvironment(ctx, params)
}

func (s *Store) GetEnvironment(ctx context.Context, id pgtype.UUID) (sqlc.Environment, error) {
	return s.queries.GetEnvironment(ctx, id)
}

func (s *Store) ListEnvironmentsByProject(ctx context.Context, projectID pgtype.UUID) ([]sqlc.Environment, error) {
	return s.queries.ListEnvironmentsByProject(ctx, projectID)
}

func (s *Store) UpdateEnvironment(ctx context.Context, params sqlc.UpdateEnvironmentParams) (sqlc.Environment, error) {
	return s.queries.UpdateEnvironment(ctx, params)
}

func (s *Store) DeleteEnvironmentGuarded(ctx context.Context, environmentID pgtype.UUID) (removed bool, orphanVolumes []string, err error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	env, err := queries.GetEnvironment(ctx, environmentID)
	if err != nil {
		return false, nil, err
	}
	if _, err := queries.GetProjectForUpdate(ctx, env.ProjectID); err != nil {
		return false, nil, err
	}
	count, err := queries.CountEnvironmentsByProject(ctx, env.ProjectID)
	if err != nil {
		return false, nil, err
	}
	if count <= 1 {
		return false, nil, nil
	}
	if _, err := queries.PurgeTombstonedServicesByEnvironment(ctx, environmentID); err != nil {
		return false, nil, err
	}
	volumes, err := queries.PurgeTombstonedDatabasesByEnvironment(ctx, environmentID)
	if err != nil {
		return false, nil, err
	}
	affected, err := queries.DeleteEnvironment(ctx, environmentID)
	if err != nil {
		return false, nil, err
	}
	if affected == 0 {
		return false, nil, pgx.ErrNoRows
	}
	if err := tx.Commit(ctx); err != nil {
		return false, nil, err
	}
	return true, volumes, nil
}

func (s *Store) CountEnvironmentsByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountEnvironmentsByProject(ctx, projectID)
}

func (s *Store) ListSharedVariables(ctx context.Context, projectID, environmentID pgtype.UUID) ([]sqlc.SharedVariable, error) {
	return s.queries.ListSharedVariables(ctx, sqlc.ListSharedVariablesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
}

func (s *Store) ListSharedVariablesForEnvironment(ctx context.Context, projectID, environmentID pgtype.UUID) ([]sqlc.SharedVariable, error) {
	return s.queries.ListSharedVariablesForEnvironment(ctx, sqlc.ListSharedVariablesForEnvironmentParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
}

func (s *Store) ReplaceSharedVariablesLocked(ctx context.Context, projectID, environmentID pgtype.UUID, build func(existing []sqlc.SharedVariable) ([]sqlc.InsertSharedVariableParams, error)) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exist pgtype.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&exist); err != nil {
		return err
	}
	if environmentID.Valid {
		var parent pgtype.UUID
		if err := tx.QueryRow(ctx,
			`SELECT project_id FROM environments WHERE id = $1 FOR UPDATE`, environmentID).Scan(&parent); err != nil {
			return err
		}
		if parent != projectID {
			return pgx.ErrNoRows
		}
	}

	queries := s.queries.WithTx(tx)
	existing, err := queries.ListSharedVariables(ctx, sqlc.ListSharedVariablesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return err
	}
	params, err := build(existing)
	if err != nil {
		return err
	}
	if err := queries.DeleteSharedVariables(ctx, sqlc.DeleteSharedVariablesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	}); err != nil {
		return err
	}
	for _, variable := range params {
		if _, err := queries.InsertSharedVariable(ctx, variable); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListPreviewBases(ctx context.Context, previewAppIDs []pgtype.UUID) ([]sqlc.ListPreviewBasesRow, error) {
	return s.queries.ListPreviewBases(ctx, previewAppIDs)
}
