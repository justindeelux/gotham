package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateService(ctx context.Context, params sqlc.CreateServiceParams) (sqlc.Service, error) {
	return s.queries.CreateService(ctx, params)
}

func (s *Store) GetService(ctx context.Context, id pgtype.UUID) (sqlc.Service, error) {
	return s.queries.GetService(ctx, id)
}

func (s *Store) ListServicesByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByUser(ctx, userID)
}

func (s *Store) ListServicesByEnvironment(ctx context.Context, environmentID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByEnvironment(ctx, environmentID)
}

func (s *Store) ListServicesByProject(ctx context.Context, projectID pgtype.UUID) ([]sqlc.Service, error) {
	return s.queries.ListServicesByProject(ctx, projectID)
}

func (s *Store) CountServicesByEnvironment(ctx context.Context, environmentID pgtype.UUID) (int64, error) {
	return s.queries.CountServicesByEnvironment(ctx, environmentID)
}

func (s *Store) CountServicesByProject(ctx context.Context, projectID pgtype.UUID) (int64, error) {
	return s.queries.CountServicesByProject(ctx, projectID)
}

func (s *Store) ListServicesByServer(ctx context.Context, serverID pgtype.UUID) ([]sqlc.ListServicesByServerRow, error) {
	return s.queries.ListServicesByServer(ctx, serverID)
}

func (s *Store) ServiceNameInEnvironment(ctx context.Context, params sqlc.ServiceNameInEnvironmentParams) (bool, error) {
	return s.queries.ServiceNameInEnvironment(ctx, params)
}

func (s *Store) HasActiveServiceDeploy(ctx context.Context, serviceID pgtype.UUID) (bool, error) {
	return s.queries.HasActiveServiceDeploy(ctx, serviceID)
}

func (s *Store) HasServiceDeploys(ctx context.Context, serviceID pgtype.UUID) (bool, error) {
	return s.queries.HasServiceDeploys(ctx, serviceID)
}

func (s *Store) UpdateServiceConfig(ctx context.Context, params sqlc.UpdateServiceConfigParams) (sqlc.Service, error) {
	return s.queries.UpdateServiceConfig(ctx, params)
}

func (s *Store) UpdateServiceStatus(ctx context.Context, params sqlc.UpdateServiceStatusParams) (sqlc.Service, error) {
	return s.queries.UpdateServiceStatus(ctx, params)
}

func (s *Store) ListRoutableServices(ctx context.Context) ([]sqlc.Service, error) {
	return s.queries.ListRoutableServices(ctx)
}

func (s *Store) SoftDeleteService(ctx context.Context, id pgtype.UUID) (sqlc.Service, error) {
	return s.queries.SoftDeleteService(ctx, id)
}

func (s *Store) CreateServiceDeploy(ctx context.Context, params sqlc.CreateServiceDeployParams) (sqlc.ServiceDeploy, error) {
	return s.queries.CreateServiceDeploy(ctx, params)
}

func (s *Store) UpdateServiceDeploy(ctx context.Context, params sqlc.UpdateServiceDeployParams) (sqlc.ServiceDeploy, error) {
	return s.queries.UpdateServiceDeploy(ctx, params)
}

func (s *Store) ListServiceDeploys(ctx context.Context, params sqlc.ListServiceDeploysParams) ([]sqlc.ServiceDeploy, error) {
	return s.queries.ListServiceDeploys(ctx, params)
}
