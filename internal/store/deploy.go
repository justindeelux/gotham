package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateApplication(ctx context.Context, params sqlc.CreateApplicationParams) (sqlc.Application, error) {
	return s.queries.CreateApplication(ctx, params)
}

func (s *Store) GetApplication(ctx context.Context, id pgtype.UUID) (sqlc.Application, error) {
	return s.queries.GetApplication(ctx, id)
}

func (s *Store) ListApplicationsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Application, error) {
	return s.queries.ListApplicationsByUser(ctx, userID)
}

func (s *Store) ListApplicationsByEnvironment(ctx context.Context, params sqlc.ListApplicationsByEnvironmentParams) ([]sqlc.Application, error) {
	return s.queries.ListApplicationsByEnvironment(ctx, params)
}

func (s *Store) ListApplicationsByProject(ctx context.Context, params sqlc.ListApplicationsByProjectParams) ([]sqlc.Application, error) {
	return s.queries.ListApplicationsByProject(ctx, params)
}

func (s *Store) CountApplicationsByEnvironment(ctx context.Context, environmentID pgtype.UUID) (int64, error) {
	return s.queries.CountApplicationsByEnvironment(ctx, environmentID)
}

func (s *Store) CountApplicationsByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountApplicationsByProject(ctx, projectID)
}

func (s *Store) CountPreviewApplicationsByEnvironment(ctx context.Context, environmentID pgtype.UUID) (int64, error) {
	return s.queries.CountPreviewApplicationsByEnvironment(ctx, environmentID)
}

func (s *Store) CountPreviewApplicationsByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountPreviewApplicationsByProject(ctx, projectID)
}

func (s *Store) ListApplicationsByServer(ctx context.Context, serverID pgtype.UUID) ([]sqlc.ListApplicationsByServerRow, error) {
	return s.queries.ListApplicationsByServer(ctx, serverID)
}

func (s *Store) ApplicationNameInEnvironment(ctx context.Context, params sqlc.ApplicationNameInEnvironmentParams) (bool, error) {
	return s.queries.ApplicationNameInEnvironment(ctx, params)
}

func (s *Store) UpdateApplication(ctx context.Context, params sqlc.UpdateApplicationParams) (sqlc.Application, error) {
	return s.queries.UpdateApplication(ctx, params)
}

func (s *Store) DeleteApplication(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteApplication(ctx, id)
}

func (s *Store) CreateApplicationWithConfig(
	ctx context.Context,
	params sqlc.CreateApplicationParams,
	envVars []sqlc.InsertEnvVarParams,
	secrets []sqlc.InsertSecretParams,
	storages []sqlc.InsertStorageParams,
) (sqlc.Application, error) {
	if requestreplay.From(ctx) != nil {
		return s.createApplicationReplay(ctx, params, envVars, secrets, storages)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Application{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	app, err := createApplicationWithConfigTx(ctx, queries, params, envVars, secrets, storages)
	if err != nil {
		return sqlc.Application{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Application{}, err
	}
	return app, nil
}

func (s *Store) CreateApplicationWithConfigAndDomain(
	ctx context.Context,
	params sqlc.CreateApplicationParams,
	envVars []sqlc.InsertEnvVarParams,
	secrets []sqlc.InsertSecretParams,
	storages []sqlc.InsertStorageParams,
	domain *sqlc.CreateApplicationDomainParams,
) (sqlc.Application, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Application{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	app, err := createApplicationWithConfigTx(ctx, queries, params, envVars, secrets, storages)
	if err != nil {
		return sqlc.Application{}, err
	}
	if domain != nil {
		row := *domain
		row.ApplicationID = app.ID
		if _, err := queries.CreateApplicationDomain(ctx, row); err != nil {
			return sqlc.Application{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Application{}, err
	}
	return app, nil
}

func createApplicationWithConfigTx(
	ctx context.Context,
	queries *sqlc.Queries,
	params sqlc.CreateApplicationParams,
	envVars []sqlc.InsertEnvVarParams,
	secrets []sqlc.InsertSecretParams,
	storages []sqlc.InsertStorageParams,
) (sqlc.Application, error) {
	app, err := queries.CreateApplication(ctx, params)
	if err != nil {
		return sqlc.Application{}, err
	}
	for _, row := range envVars {
		row.ApplicationID = app.ID
		if _, err := queries.InsertEnvVar(ctx, row); err != nil {
			return sqlc.Application{}, err
		}
	}
	for _, row := range secrets {
		row.ApplicationID = app.ID
		if _, err := queries.InsertSecret(ctx, row); err != nil {
			return sqlc.Application{}, err
		}
	}
	for _, row := range storages {
		row.ApplicationID = app.ID
		if _, err := queries.InsertStorage(ctx, row); err != nil {
			return sqlc.Application{}, err
		}
	}
	return app, nil
}

func (s *Store) ReplaceApplicationEnv(
	ctx context.Context,
	applicationID pgtype.UUID,
	envVars []sqlc.InsertEnvVarParams,
	secrets []sqlc.InsertSecretParams,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.LockApplication(ctx, applicationID); err != nil {
		return err
	}
	if s.BeforeCollectionClear != nil {
		s.BeforeCollectionClear()
	}
	if err := queries.ClearEnvVarsByApp(ctx, applicationID); err != nil {
		return err
	}
	if err := queries.ClearSecretsByApp(ctx, applicationID); err != nil {
		return err
	}
	for _, row := range envVars {
		row.ApplicationID = applicationID
		if _, err := queries.InsertEnvVar(ctx, row); err != nil {
			return err
		}
	}
	for _, row := range secrets {
		row.ApplicationID = applicationID
		if _, err := queries.InsertSecret(ctx, row); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ReplaceApplicationStorages(
	ctx context.Context,
	applicationID pgtype.UUID,
	storages []sqlc.InsertStorageParams,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.LockApplication(ctx, applicationID); err != nil {
		return err
	}
	if s.BeforeCollectionClear != nil {
		s.BeforeCollectionClear()
	}
	if err := queries.ClearStoragesByApp(ctx, applicationID); err != nil {
		return err
	}
	for _, row := range storages {
		row.ApplicationID = applicationID
		if _, err := queries.InsertStorage(ctx, row); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateDeployment(ctx context.Context, params sqlc.CreateDeploymentParams) (sqlc.Deployment, error) {
	if requestreplay.From(ctx) != nil {
		return s.createDeploymentReplay(ctx, params)
	}
	return s.queries.CreateDeployment(ctx, params)
}

func (s *Store) GetDeployment(ctx context.Context, id, applicationID pgtype.UUID) (sqlc.Deployment, error) {
	return s.queries.GetDeployment(ctx, sqlc.GetDeploymentParams{ID: id, ApplicationID: applicationID})
}

func (s *Store) ListDeploymentsByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.Deployment, error) {
	return s.queries.ListDeploymentsByApp(ctx, applicationID)
}

func (s *Store) ListDeploymentsByAppLimit(ctx context.Context, applicationID pgtype.UUID, limit int32) ([]sqlc.Deployment, error) {
	return s.queries.ListDeploymentsByAppLimit(ctx, sqlc.ListDeploymentsByAppLimitParams{
		ApplicationID: applicationID,
		Limit:         limit,
	})
}

func (s *Store) ListRecentDeploymentsByUser(ctx context.Context, userID pgtype.UUID, limit int32) ([]sqlc.ListRecentDeploymentsByUserRow, error) {
	return s.queries.ListRecentDeploymentsByUser(ctx, sqlc.ListRecentDeploymentsByUserParams{
		UserID: userID,
		Limit:  limit,
	})
}

func (s *Store) FailStaleDeployments(ctx context.Context) (int64, error) {
	return s.queries.FailStaleDeployments(ctx)
}

func (s *Store) UpdateDeployment(ctx context.Context, params sqlc.UpdateDeploymentParams) (sqlc.Deployment, error) {
	return s.queries.UpdateDeployment(ctx, params)
}

func (s *Store) UpdateDeploymentBuildLog(ctx context.Context, params sqlc.UpdateDeploymentBuildLogParams) error {
	return s.queries.UpdateDeploymentBuildLog(ctx, params)
}

func (s *Store) ListEnvVarsByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.EnvVar, error) {
	return s.queries.ListEnvVarsByApp(ctx, applicationID)
}

func (s *Store) ListSecretsByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.Secret, error) {
	return s.queries.ListSecretsByApp(ctx, applicationID)
}

func (s *Store) ListStoragesByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.Storage, error) {
	return s.queries.ListStoragesByApp(ctx, applicationID)
}

func (s *Store) ListEnvConfigByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.EnvVar, []sqlc.Secret, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	envVars, err := queries.ListEnvVarsByApp(ctx, applicationID)
	if err != nil {
		return nil, nil, err
	}
	if s.AfterEnvReadBeforeSecrets != nil {
		s.AfterEnvReadBeforeSecrets()
	}
	secrets, err := queries.ListSecretsByApp(ctx, applicationID)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return envVars, secrets, nil
}
