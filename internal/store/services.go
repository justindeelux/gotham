package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateService stores a compose-service row with the caller-generated ID and
// returns it. env is the JSON-encoded substitution environment.
func (s *Store) CreateService(ctx context.Context, params sqlc.CreateServiceParams) (sqlc.Service, error) {
	return s.queries.CreateService(ctx, params)
}

// GetService returns a live service (soft-deleted rows are invisible) with the
// given ID; ownership is checked by the caller.
func (s *Store) GetService(ctx context.Context, id pgtype.UUID) (sqlc.Service, error) {
	return s.queries.GetService(ctx, id)
}

// ListServicesByUser returns every live service owned by userID, newest first.
func (s *Store) ListServicesByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByUser(ctx, userID)
}

// UpdateServiceConfig persists the mutable service configuration (name,
// document, environment) without touching the status column, so a lifecycle
// completion and a configuration edit cannot overwrite each other.
func (s *Store) UpdateServiceConfig(ctx context.Context, params sqlc.UpdateServiceConfigParams) (sqlc.Service, error) {
	return s.queries.UpdateServiceConfig(ctx, params)
}

// UpdateServiceStatus writes only the status column of a live service.
func (s *Store) UpdateServiceStatus(ctx context.Context, params sqlc.UpdateServiceStatusParams) (sqlc.Service, error) {
	return s.queries.UpdateServiceStatus(ctx, params)
}

// ListRoutableServices returns every live service in creation order. The
// proxy source renders each row's domain map from its current document and
// environment; no rendered state is stored twice.
func (s *Store) ListRoutableServices(ctx context.Context) ([]sqlc.Service, error) {
	return s.queries.ListRoutableServices(ctx)
}

// SoftDeleteService marks a service deleted without touching its named
// volumes, so the data survives. An already-deleted row reports pgx.ErrNoRows.
func (s *Store) SoftDeleteService(ctx context.Context, id pgtype.UUID) (sqlc.Service, error) {
	return s.queries.SoftDeleteService(ctx, id)
}

// CreateServiceDeploy stores one deploy attempt whose ComposeYaml is the
// rendered document exactly as it was written to the node.
func (s *Store) CreateServiceDeploy(ctx context.Context, params sqlc.CreateServiceDeployParams) (sqlc.ServiceDeploy, error) {
	return s.queries.CreateServiceDeploy(ctx, params)
}

// UpdateServiceDeploy records the state, redacted error and finish time of one
// deploy attempt.
func (s *Store) UpdateServiceDeploy(ctx context.Context, params sqlc.UpdateServiceDeployParams) (sqlc.ServiceDeploy, error) {
	return s.queries.UpdateServiceDeploy(ctx, params)
}

// ListServiceDeploys returns a service's deploy attempts, newest first,
// bounded by the query's limit.
func (s *Store) ListServiceDeploys(ctx context.Context, params sqlc.ListServiceDeploysParams) ([]sqlc.ServiceDeploy, error) {
	return s.queries.ListServiceDeploys(ctx, params)
}
