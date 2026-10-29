package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Repository interface {
	CreateService(ctx context.Context, service Service) (Service, error)
	GetService(ctx context.Context, serviceID uuid.UUID) (Service, error)
	ListServices(ctx context.Context, userID uuid.UUID) ([]Service, error)
	ListServicesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Service, error)
	ListServicesByProject(ctx context.Context, projectID uuid.UUID) ([]Service, error)
	NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error)
	ResolveEnvironment(ctx context.Context, environmentID uuid.UUID) (EnvironmentRef, error)
	ResolveProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error)
	HasActiveDeploy(ctx context.Context, serviceID uuid.UUID) (bool, error)
	HasDeploys(ctx context.Context, serviceID uuid.UUID) (bool, error)
	UpdateServiceConfig(ctx context.Context, service Service) (Service, error)
	UpdateServiceStatus(ctx context.Context, serviceID uuid.UUID, status Status) (Service, error)
	SoftDeleteService(ctx context.Context, serviceID uuid.UUID) (Service, error)
	CreateServiceDeploy(ctx context.Context, deploy Deploy) (Deploy, error)
	UpdateServiceDeploy(ctx context.Context, deploy Deploy) (Deploy, error)
	ListServiceDeploys(ctx context.Context, serviceID uuid.UUID, limit int32) ([]Deploy, error)
	ListServiceDeploysByState(ctx context.Context, state DeployState) ([]Deploy, error)
	ServerExists(ctx context.Context, serverID uuid.UUID) (bool, error)
}

type storeRepository struct {
	store *store.Store
}

var _ Repository = (*storeRepository)(nil)

func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

func (r *storeRepository) CreateService(ctx context.Context, service Service) (Service, error) {
	env, err := marshalEnv(service.Env)
	if err != nil {
		return Service{}, err
	}
	row, err := r.store.CreateService(ctx, sqlc.CreateServiceParams{
		ID:            pgUUID(service.ID),
		UserID:        pgUUID(service.UserID),
		ServerID:      pgUUID(service.ServerID),
		EnvironmentID: pgUUID(service.EnvironmentID),
		Name:          service.Name,
		Status:        string(service.Status),
		ComposeYaml:   service.ComposeYAML,
		Env:           env,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Service{}, ErrConflict
		}
		if isForeignKeyViolation(err) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("services: create service: %w", err)
	}
	stored, convErr := serviceFromRow(row)
	if convErr != nil {
		return Service{}, convErr
	}
	enriched, err := r.enrich(ctx, []Service{stored})
	if err != nil {
		return Service{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) GetService(ctx context.Context, serviceID uuid.UUID) (Service, error) {
	row, err := r.store.GetService(ctx, pgUUID(serviceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("services: get service: %w", err)
	}
	stored, convErr := serviceFromRow(row)
	if convErr != nil {
		return Service{}, convErr
	}
	enriched, err := r.enrich(ctx, []Service{stored})
	if err != nil {
		return Service{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) ListServices(ctx context.Context, userID uuid.UUID) ([]Service, error) {
	rows, err := r.store.ListServicesByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("services: list services: %w", err)
	}
	services := make([]Service, 0, len(rows))
	for _, row := range rows {
		service, err := serviceFromRow(row)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return r.enrich(ctx, services)
}

func (r *storeRepository) ListServicesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Service, error) {
	rows, err := r.store.ListServicesByEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		return nil, fmt.Errorf("services: list services by environment: %w", err)
	}
	services := make([]Service, 0, len(rows))
	for _, row := range rows {
		service, err := serviceFromRow(row)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return r.enrich(ctx, services)
}

func (r *storeRepository) ListServicesByProject(ctx context.Context, projectID uuid.UUID) ([]Service, error) {
	rows, err := r.store.ListServicesByProject(ctx, pgUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("services: list services by project: %w", err)
	}
	services := make([]Service, 0, len(rows))
	for _, row := range rows {
		service, err := serviceFromRow(row)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return r.enrich(ctx, services)
}

func (r *storeRepository) NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error) {
	collision, err := r.store.ServiceNameInEnvironment(ctx, sqlc.ServiceNameInEnvironmentParams{
		EnvironmentID: pgUUID(environmentID),
		Name:          name,
		ID:            pgUUID(exceptID),
	})
	if err != nil {
		return false, fmt.Errorf("services: name in environment: %w", err)
	}
	return collision, nil
}

func (r *storeRepository) ResolveEnvironment(ctx context.Context, environmentID uuid.UUID) (EnvironmentRef, error) {
	row, err := r.store.GetEnvironment(ctx, pgUUID(environmentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EnvironmentRef{}, ErrNotFound
		}
		return EnvironmentRef{}, fmt.Errorf("services: get environment: %w", err)
	}
	return EnvironmentRef{
		ID:        uuidFromPG(row.ID),
		ProjectID: uuidFromPG(row.ProjectID),
		Name:      row.Name,
	}, nil
}

func (r *storeRepository) ResolveProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	if _, err := r.store.GetProject(ctx, pgUUID(projectID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("services: get project: %w", err)
	}
	return projectID, nil
}

func (r *storeRepository) HasActiveDeploy(ctx context.Context, serviceID uuid.UUID) (bool, error) {
	active, err := r.store.HasActiveServiceDeploy(ctx, pgUUID(serviceID))
	if err != nil {
		return false, fmt.Errorf("services: active deploy check: %w", err)
	}
	return active, nil
}

func (r *storeRepository) HasDeploys(ctx context.Context, serviceID uuid.UUID) (bool, error) {
	deployed, err := r.store.HasServiceDeploys(ctx, pgUUID(serviceID))
	if err != nil {
		return false, fmt.Errorf("services: deploy history check: %w", err)
	}
	return deployed, nil
}

func (r *storeRepository) enrich(ctx context.Context, services []Service) ([]Service, error) {
	envs := make(map[uuid.UUID]sqlc.Environment)
	projects := make(map[uuid.UUID]sqlc.Project)
	servers := make(map[uuid.UUID]string)
	for i, service := range services {
		if service.EnvironmentID != uuid.Nil {
			env, ok := envs[service.EnvironmentID]
			if !ok {
				fetched, err := r.store.GetEnvironment(ctx, pgUUID(service.EnvironmentID))
				if err != nil {
					return nil, fmt.Errorf("services: get environment: %w", err)
				}
				env, envs[service.EnvironmentID] = fetched, fetched
			}
			services[i].EnvironmentName = env.Name
			services[i].ProjectID = uuidFromPG(env.ProjectID)
			if projectID := uuidFromPG(env.ProjectID); projectID != uuid.Nil {
				project, ok := projects[projectID]
				if !ok {
					fetched, err := r.store.GetProject(ctx, pgUUID(projectID))
					if err != nil {
						return nil, fmt.Errorf("services: get project: %w", err)
					}
					project, projects[projectID] = fetched, fetched
				}
				services[i].ProjectName = project.Name
			}
		}
		if service.ServerID != uuid.Nil {
			name, ok := servers[service.ServerID]
			if !ok {
				server, err := r.store.GetServerByID(ctx, pgUUID(service.ServerID))
				if err != nil {
					return nil, fmt.Errorf("services: get server: %w", err)
				}
				name, servers[service.ServerID] = server.Name, server.Name
			}
			services[i].ServerName = name
		}
	}
	return services, nil
}

func (r *storeRepository) UpdateServiceConfig(ctx context.Context, service Service) (Service, error) {
	env, err := marshalEnv(service.Env)
	if err != nil {
		return Service{}, err
	}
	row, err := r.store.UpdateServiceConfig(ctx, sqlc.UpdateServiceConfigParams{
		ID:            pgUUID(service.ID),
		Name:          service.Name,
		ComposeYaml:   service.ComposeYAML,
		Env:           env,
		EnvironmentID: pgUUID(service.EnvironmentID),
		ServerID:      pgUUID(service.ServerID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Service{}, ErrConflict
		}
		if isForeignKeyViolation(err) {
			return Service{}, ErrNotFound
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("services: update service config: %w", err)
	}
	stored, convErr := serviceFromRow(row)
	if convErr != nil {
		return Service{}, convErr
	}
	enriched, err := r.enrich(ctx, []Service{stored})
	if err != nil {
		return Service{}, err
	}
	return enriched[0], nil
}

func (r *storeRepository) UpdateServiceStatus(ctx context.Context, serviceID uuid.UUID, status Status) (Service, error) {
	row, err := r.store.UpdateServiceStatus(ctx, sqlc.UpdateServiceStatusParams{
		ID:     pgUUID(serviceID),
		Status: string(status),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("services: update service status: %w", err)
	}
	return serviceFromRow(row)
}

func (r *storeRepository) SoftDeleteService(ctx context.Context, serviceID uuid.UUID) (Service, error) {
	row, err := r.store.SoftDeleteService(ctx, pgUUID(serviceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("services: soft delete service: %w", err)
	}
	return serviceFromRow(row)
}

func (r *storeRepository) CreateServiceDeploy(ctx context.Context, deploy Deploy) (Deploy, error) {
	row, err := r.store.CreateServiceDeploy(ctx, sqlc.CreateServiceDeployParams{
		ID:          pgUUID(deploy.ID),
		ServiceID:   pgUUID(deploy.ServiceID),
		State:       string(deploy.State),
		ComposeYaml: deploy.ComposeYAML,
	})
	if err != nil {
		return Deploy{}, fmt.Errorf("services: create deploy: %w", err)
	}
	return deployFromRow(row), nil
}

func (r *storeRepository) UpdateServiceDeploy(ctx context.Context, deploy Deploy) (Deploy, error) {
	row, err := r.store.UpdateServiceDeploy(ctx, sqlc.UpdateServiceDeployParams{
		ID:         pgUUID(deploy.ID),
		State:      string(deploy.State),
		Error:      deploy.Error,
		FinishedAt: pgTime(deploy.FinishedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deploy{}, ErrNotFound
		}
		return Deploy{}, fmt.Errorf("services: update deploy: %w", err)
	}
	return deployFromRow(row), nil
}

func (r *storeRepository) ListServiceDeploys(ctx context.Context, serviceID uuid.UUID, limit int32) ([]Deploy, error) {
	if limit <= 0 || limit > deployHistoryLimit {
		limit = deployHistoryLimit
	}
	rows, err := r.store.ListServiceDeploys(ctx, sqlc.ListServiceDeploysParams{
		ServiceID: pgUUID(serviceID),
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("services: list deploys: %w", err)
	}
	deploys := make([]Deploy, 0, len(rows))
	for _, row := range rows {
		deploys = append(deploys, deployFromRow(row))
	}
	return deploys, nil
}

func (r *storeRepository) ListServiceDeploysByState(ctx context.Context, state DeployState) ([]Deploy, error) {
	rows, err := r.store.ListServiceDeploysByState(ctx, string(state))
	if err != nil {
		return nil, fmt.Errorf("services: list deploys by state: %w", err)
	}
	deploys := make([]Deploy, 0, len(rows))
	for _, row := range rows {
		deploys = append(deploys, deployFromRow(row))
	}
	return deploys, nil
}

func (r *storeRepository) ServerExists(ctx context.Context, serverID uuid.UUID) (bool, error) {
	if serverID == uuid.Nil {
		return false, nil
	}
	if _, err := r.store.GetServerByID(ctx, pgUUID(serverID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("services: resolve server: %w", err)
	}
	return true, nil
}

func serviceFromRow(row sqlc.Service) (Service, error) {
	env, err := unmarshalEnv(row.Env)
	if err != nil {
		return Service{}, err
	}
	return Service{
		ID:            uuidFromPG(row.ID),
		UserID:        uuidFromPG(row.UserID),
		ServerID:      uuidFromPG(row.ServerID),
		EnvironmentID: uuidFromPG(row.EnvironmentID),
		Name:          row.Name,
		Status:        Status(row.Status),
		ComposeYAML:   row.ComposeYaml,
		Env:           env,
		CreatedAt:     timeFromPG(row.CreatedAt),
		UpdatedAt:     timeFromPG(row.UpdatedAt),
		DeletedAt:     timeFromPG(row.DeletedAt),
	}, nil
}

func deployFromRow(row sqlc.ServiceDeploy) Deploy {
	return Deploy{
		ID:          uuidFromPG(row.ID),
		ServiceID:   uuidFromPG(row.ServiceID),
		State:       DeployState(row.State),
		ComposeYAML: row.ComposeYaml,
		Error:       row.Error,
		CreatedAt:   timeFromPG(row.CreatedAt),
		UpdatedAt:   timeFromPG(row.UpdatedAt),
		FinishedAt:  timeFromPG(row.FinishedAt),
	}
}

func marshalEnv(env map[string]string) ([]byte, error) {
	if env == nil {
		env = map[string]string{}
	}
	if len(env) > MaxEnvVars {
		return nil, fmt.Errorf("%w: at most %d environment variables", ErrValidation, MaxEnvVars)
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("services: encode environment: %w", err)
	}
	return encoded, nil
}

func unmarshalEnv(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	env := map[string]string{}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("services: decode environment: %w", err)
	}
	return env, nil
}

func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgTime(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
