package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateApplication stores an application and returns the row.
func (s *Store) CreateApplication(ctx context.Context, params sqlc.CreateApplicationParams) (sqlc.Application, error) {
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
