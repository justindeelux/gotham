package store

import (
	"context"

	"github.com/jackc/pgx/v5"
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

// DeleteProject removes one project of a team in a transaction and reports
// how many rows were removed (0 when the ID is unknown or foreign).
// Environments cascade. Soft-deleted services and databases of every
// environment are purged in the same transaction first, so only live
// resources block the delete (see DeleteEnvironmentGuarded); purged
// database volumes may still exist on the node and come back as
// orphanVolumes for the caller to log.
func (s *Store) DeleteProject(ctx context.Context, params sqlc.DeleteProjectParams) (int64, []string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	// Team-scoped existence first: a foreign project ID must neither purge
	// nor delete, and reports zero rows like the old single-query delete.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND team_id = $2)`,
		params.ID, params.TeamID).Scan(&exists); err != nil {
		return 0, nil, err
	}
	if !exists {
		// Rollback below; report zero rows removed.
		return 0, nil, nil
	}
	if _, err := queries.PurgeTombstonedServicesByProject(ctx, params.ID); err != nil {
		return 0, nil, err
	}
	volumes, err := queries.PurgeTombstonedDatabasesByProject(ctx, params.ID)
	if err != nil {
		return 0, nil, err
	}
	affected, err := queries.DeleteProject(ctx, params)
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return affected, volumes, nil
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

// DeleteEnvironmentGuarded removes one environment of a team in a
// transaction that first locks the parent project row (SELECT ... FOR
// UPDATE), and reports whether the row was removed. The order is always
// project first, then environment — ReplaceSharedVariablesLocked takes the
// same order, so a variable PUT racing this delete serializes instead of
// deadlocking. removed=false means the
// delete would leave the project without an environment, so nothing was
// deleted and the caller must refuse with its last-environment error.
// pgx.ErrNoRows means the environment is unknown or foreign. A foreign-key
// refusal (PE-2's RESTRICT against a racing resource insert) propagates for
// the caller to map.
//
// Soft-deleted services and databases of the environment are purged in the
// same transaction first, so only live resources block the delete: a
// tombstone must never pin its environment forever (there is no other purge
// path for services, and database retention only sees live rows). Purged
// database volumes may still exist on the node; their storage paths come
// back as orphanVolumes for the caller to log.
func (s *Store) DeleteEnvironmentGuarded(ctx context.Context, teamID, environmentID pgtype.UUID) (removed bool, orphanVolumes []string, err error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	env, err := queries.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
		ID:     environmentID,
		TeamID: teamID,
	})
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
	affected, err := queries.DeleteEnvironment(ctx, sqlc.DeleteEnvironmentParams{
		ID:     environmentID,
		TeamID: teamID,
	})
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

// CountEnvironmentsByProject reports how many environments a project holds.
// Deleting the last one is refused, so a project always has at least one.
func (s *Store) CountEnvironmentsByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountEnvironmentsByProject(ctx, projectID)
}

// ListSharedVariables returns one scope's shared variables (an invalid
// environment ID reads the project level), ordered by key.
func (s *Store) ListSharedVariables(ctx context.Context, projectID, environmentID pgtype.UUID) ([]sqlc.SharedVariable, error) {
	return s.queries.ListSharedVariables(ctx, sqlc.ListSharedVariablesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
}

// ListSharedVariablesForEnvironment returns the project-level rows plus one
// environment's rows in a single snapshot, so the deploy merge reads both
// scopes without a concurrent replace slipping between two reads.
func (s *Store) ListSharedVariablesForEnvironment(ctx context.Context, projectID, environmentID pgtype.UUID) ([]sqlc.SharedVariable, error) {
	return s.queries.ListSharedVariablesForEnvironment(ctx, sqlc.ListSharedVariablesForEnvironmentParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
}

// ReplaceSharedVariablesLocked swaps one scope's whole set in a transaction
// that first locks the scope parent: the project row for the project level,
// the project row and then the environment row for an environment scope.
// The order is always project first, then environment — the same order
// DeleteEnvironmentGuarded and DeleteProject take — so a PUT racing an
// environment or project delete serializes instead of deadlocking. The
// existing set is read inside the same transaction so the keep-ciphertext
// path cannot restore a value a concurrent PUT replaced. A missing parent
// answers pgx.ErrNoRows; a parent deleted by a racing transaction trips the
// foreign key, which the caller maps.
func (s *Store) ReplaceSharedVariablesLocked(ctx context.Context, projectID, environmentID pgtype.UUID, build func(existing []sqlc.SharedVariable) ([]sqlc.InsertSharedVariableParams, error)) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Project first, then environment (see the lock-order note above): the
	// environment row alone is not enough, because each insert's foreign-key
	// check takes the project row next while the deletes hold it first.
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

// ListPreviewBases maps preview application rows to their base application
// through preview_deploys (PE-5 M1), scoped to the team by the caller.
func (s *Store) ListPreviewBases(ctx context.Context, params sqlc.ListPreviewBasesParams) ([]sqlc.ListPreviewBasesRow, error) {
	return s.queries.ListPreviewBases(ctx, params)
}
