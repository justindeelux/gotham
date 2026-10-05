package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateService stores a compose-service row with the caller-generated ID and
// returns it. env is the JSON-encoded substitution environment.
func (s *Store) CreateService(ctx context.Context, params sqlc.CreateServiceParams) (sqlc.Service, error) {
	params.TeamID = personalTeamOrDefault(params.TeamID, params.UserID)
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

// ListServicesByTeam returns every live service of one team, newest first.
func (s *Store) ListServicesByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByTeam(ctx, teamID)
}

// ListServicesByEnvironment returns one environment's live services, newest
// first.
func (s *Store) ListServicesByEnvironment(ctx context.Context, environmentID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByEnvironment(ctx, environmentID)
}

// ListServicesByProject returns every environment's live services of one
// project, newest first.
func (s *Store) ListServicesByProject(ctx context.Context, projectID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByProject(ctx, projectID)
}

// CountServicesByEnvironment tallies one environment's live services.
func (s *Store) CountServicesByEnvironment(ctx context.Context, environmentID pgtype.UUID) (int64, error) {
	return s.queries.CountServicesByEnvironment(ctx, environmentID)
}

// CountServicesByProject tallies every environment's live services of one
// project.
func (s *Store) CountServicesByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountServicesByProject(ctx, projectID)
}

// ListServicesByServer returns one node's live services (id and name) for
// the server-delete 409.
func (s *Store) ListServicesByServer(ctx context.Context, serverID pgtype.UUID) ([]sqlc.ListServicesByServerRow, error) {
	return s.queries.ListServicesByServer(ctx, serverID)
}

// ServiceNameInEnvironment reports whether the environment holds another live
// service with the name (the move-collision pre-check).
func (s *Store) ServiceNameInEnvironment(ctx context.Context, params sqlc.ServiceNameInEnvironmentParams) (bool, error) {
	return s.queries.ServiceNameInEnvironment(ctx, params)
}

// HasActiveServiceDeploy reports whether a service deploy is in flight, so a
// server change can be refused while one runs.
func (s *Store) HasActiveServiceDeploy(ctx context.Context, serviceID pgtype.UUID) (bool, error) {
	return s.queries.HasActiveServiceDeploy(ctx, serviceID)
}

// HasServiceDeploys reports whether the service was ever deployed, so a
// server change can be refused once its compose project runs on a node.
func (s *Store) HasServiceDeploys(ctx context.Context, serviceID pgtype.UUID) (bool, error) {
	return s.queries.HasServiceDeploys(ctx, serviceID)
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
