package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateApplication stores an application and returns the row.
func (s *Store) CreateApplication(ctx context.Context, params sqlc.CreateApplicationParams) (sqlc.Application, error) {
	params.TeamID = personalTeamOrDefault(params.TeamID, params.UserID)
	return s.queries.CreateApplication(ctx, params)
}

// GetApplication returns the application with the given ID (ownership is
// checked by the caller).
func (s *Store) GetApplication(ctx context.Context, id pgtype.UUID) (sqlc.Application, error) {
	return s.queries.GetApplication(ctx, id)
}

// ListApplicationsByUser returns every application owned by userID, newest
// first.
func (s *Store) ListApplicationsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Application, error) {
	return s.queries.ListApplicationsByUser(ctx, userID)
}

// ListApplicationsByTeam returns every application of one team, newest first.
func (s *Store) ListApplicationsByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Application, error) {
	return s.queries.ListApplicationsByTeam(ctx, teamID)
}

// UpdateApplication persists the mutable application fields and returns the
// row. Unset columns (server_id NULL) serialise as NULL through pgUUID.
func (s *Store) UpdateApplication(ctx context.Context, params sqlc.UpdateApplicationParams) (sqlc.Application, error) {
	return s.queries.UpdateApplication(ctx, params)
}

// DeleteApplication removes an application row; env vars, secrets, storages and
// deployments cascade with it (see migration 00006).
func (s *Store) DeleteApplication(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteApplication(ctx, id)
}

// CreateApplicationWithConfig stores an application together with its env vars,
// sealed secrets and storage mappings in one transaction: a created
// application must never exist without the configuration the wizard sent with
// it. The children's application_id is filled from the inserted row, so callers
// pass the rows without an owner.
func (s *Store) CreateApplicationWithConfig(
	ctx context.Context,
	params sqlc.CreateApplicationParams,
	envVars []sqlc.InsertEnvVarParams,
	secrets []sqlc.InsertSecretParams,
	storages []sqlc.InsertStorageParams,
) (sqlc.Application, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Application{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	params.TeamID = personalTeamOrDefault(params.TeamID, params.UserID)
	queries := s.queries.WithTx(tx)
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
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Application{}, err
	}
	return app, nil
}

// ReplaceApplicationEnv replaces an application's plain env vars and sealed
// secrets in one transaction — the collections are written as a set, so a
// partial write would leave a half-updated configuration behind. Rows are
// inserted without an application_id: it is stamped from applicationID here.
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
	// Lock the parent row first: two concurrent replacements of the same
	// collection must serialize, or both clear an empty set, insert disjoint
	// keys and commit their union — a merge where a replace was asked for.
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

// ReplaceApplicationStorages replaces an application's volume map in one
// transaction (see ReplaceApplicationEnv).
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
	// Same parent lock as ReplaceApplicationEnv: concurrent storage
	// replacements serialize instead of merging (see that method).
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

// CreateDeployment stores a queued deployment and returns the row. A partial
// unique index guarantees at most one active deployment per application, so a
// concurrent submit surfaces as a unique-constraint violation.
func (s *Store) CreateDeployment(ctx context.Context, params sqlc.CreateDeploymentParams) (sqlc.Deployment, error) {
	return s.queries.CreateDeployment(ctx, params)
}

// GetDeployment returns one deployment of an application.
func (s *Store) GetDeployment(ctx context.Context, id, applicationID pgtype.UUID) (sqlc.Deployment, error) {
	return s.queries.GetDeployment(ctx, sqlc.GetDeploymentParams{ID: id, ApplicationID: applicationID})
}

// ListDeploymentsByApp returns an application's deployments, newest first.
func (s *Store) ListDeploymentsByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.Deployment, error) {
	return s.queries.ListDeploymentsByApp(ctx, applicationID)
}

// FailStaleDeployments marks deployments left non-terminal by a previous
// control plane process as failed and reports how many rows were recovered.
func (s *Store) FailStaleDeployments(ctx context.Context) (int64, error) {
	return s.queries.FailStaleDeployments(ctx)
}

// UpdateDeployment persists the mutable deployment fields and returns the row.
func (s *Store) UpdateDeployment(ctx context.Context, params sqlc.UpdateDeploymentParams) (sqlc.Deployment, error) {
	return s.queries.UpdateDeployment(ctx, params)
}

// ListEnvVarsByApp returns an application's plain environment variables, sorted
// by key.
func (s *Store) ListEnvVarsByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.EnvVar, error) {
	return s.queries.ListEnvVarsByApp(ctx, applicationID)
}

// ListSecretsByApp returns an application's sealed secrets, sorted by key. The
// ciphertext is opened by the deploy service, never here.
func (s *Store) ListSecretsByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.Secret, error) {
	return s.queries.ListSecretsByApp(ctx, applicationID)
}

// ListStoragesByApp returns an application's volume mappings, sorted by name.
func (s *Store) ListStoragesByApp(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.Storage, error) {
	return s.queries.ListStoragesByApp(ctx, applicationID)
}

// ListEnvConfigByApp returns an application's plain env vars and sealed secrets
// from one transaction, so a replace that commits between two separate reads
// cannot drop a key from either collection: both reads see the same committed
// snapshot.
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
