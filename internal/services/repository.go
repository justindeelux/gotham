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
	"github.com/justindeelux/gotham/internal/teams"
)

// Repository persists services and their deploy history. It is implemented
// over *store.Store (sqlc) in production and by fakes in tests. Soft-deleted
// rows are never returned: the queries filter deleted_at, so a deleted service
// disappears from every read until it is purged.
type Repository interface {
	// CreateService stores a new service row, or ErrConflict when a live
	// service of the same user and name exists.
	CreateService(ctx context.Context, service Service) (Service, error)
	// GetService returns a live service, or ErrNotFound.
	GetService(ctx context.Context, serviceID uuid.UUID) (Service, error)
	// ListServices returns the live services of the scope's active team (or,
	// without a team context, of the creator), newest first.
	ListServices(ctx context.Context, scope teams.Scope) ([]Service, error)
	// ListServicesByEnvironment returns one environment's live services,
	// newest first.
	ListServicesByEnvironment(ctx context.Context, environmentID uuid.UUID) ([]Service, error)
	// ListServicesByProject returns every environment's live services of one
	// project, newest first.
	ListServicesByProject(ctx context.Context, projectID uuid.UUID) ([]Service, error)
	// NameInEnvironment reports whether the environment holds another live
	// service with name (the move-collision pre-check).
	NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error)
	// ResolveEnvironment validates that environmentID belongs to teamID and
	// returns it (ErrNotFound for a foreign or missing one).
	ResolveEnvironment(ctx context.Context, environmentID, teamID uuid.UUID) (EnvironmentRef, error)
	// ResolveProject validates that projectID belongs to teamID
	// (ErrNotFound for a foreign or missing one).
	ResolveProject(ctx context.Context, projectID, teamID uuid.UUID) (uuid.UUID, error)
	// HasActiveDeploy reports whether a service deploy is in flight, so a
	// server change can be refused while one runs.
	HasActiveDeploy(ctx context.Context, serviceID uuid.UUID) (bool, error)
	// UpdateServiceConfig persists the mutable configuration fields (name,
	// document, environment) without touching the status, so a concurrent
	// lifecycle completion cannot clobber an edit (and an edit cannot clobber
	// a status).
	UpdateServiceConfig(ctx context.Context, service Service) (Service, error)
	// UpdateServiceStatus writes only the status column of a live service.
	UpdateServiceStatus(ctx context.Context, serviceID uuid.UUID, status Status) (Service, error)
	// SoftDeleteService marks the row deleted without touching any volume.
	SoftDeleteService(ctx context.Context, serviceID uuid.UUID) (Service, error)
	// CreateServiceDeploy stores one deploy attempt.
	CreateServiceDeploy(ctx context.Context, deploy Deploy) (Deploy, error)
	// UpdateServiceDeploy persists an attempt's state, error and finish time.
	UpdateServiceDeploy(ctx context.Context, deploy Deploy) (Deploy, error)
	// ListServiceDeploys returns a service's attempts, newest first, bounded
	// by limit.
	ListServiceDeploys(ctx context.Context, serviceID uuid.UUID, limit int32) ([]Deploy, error)
	// ServerExists reports whether the target node is registered AND
	// actionable by the caller's active team. A node of another team answers
	// false, like a missing one, so node IDs cannot be probed (F6); a legacy
	// node without a team stays shared.
	ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error)
}

// storeRepository adapts *store.Store to Repository.
type storeRepository struct {
	store *store.Store
}

// Compile-time guarantee.
var _ Repository = (*storeRepository)(nil)

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

// CreateService implements Repository. The partial unique index on live names
// turns a concurrent create into ErrConflict.
func (r *storeRepository) CreateService(ctx context.Context, service Service) (Service, error) {
	env, err := marshalEnv(service.Env)
	if err != nil {
		return Service{}, err
	}
	row, err := r.store.CreateService(ctx, sqlc.CreateServiceParams{
		ID:            pgUUID(service.ID),
		UserID:        pgUUID(service.UserID),
		TeamID:        pgUUID(service.TeamID),
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

// GetService implements Repository, mapping a missing row to ErrNotFound.
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

// ListServices implements Repository: the active team's live services, or the
// creator's when no team context is present.
func (r *storeRepository) ListServices(ctx context.Context, scope teams.Scope) ([]Service, error) {
	var (
		rows []sqlc.Service
		err  error
	)
	if scope.Active() {
		rows, err = r.store.ListServicesByTeam(ctx, pgUUID(scope.TeamID))
	} else {
		rows, err = r.store.ListServicesByUser(ctx, pgUUID(scope.UserID))
	}
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

// ListServicesByEnvironment implements Repository.
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

// ListServicesByProject implements Repository.
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

// NameInEnvironment implements Repository: the move-collision pre-check. The
// comparison is exact, matching the (environment_id, name) unique index.
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

// ResolveEnvironment implements Repository: a foreign or missing environment
// answers ErrNotFound, so environment IDs cannot be probed across teams.
func (r *storeRepository) ResolveEnvironment(ctx context.Context, environmentID, teamID uuid.UUID) (EnvironmentRef, error) {
	row, err := r.store.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
		ID:     pgUUID(environmentID),
		TeamID: pgUUID(teamID),
	})
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

// ResolveProject implements Repository.
func (r *storeRepository) ResolveProject(ctx context.Context, projectID, teamID uuid.UUID) (uuid.UUID, error) {
	if _, err := r.store.GetProject(ctx, sqlc.GetProjectParams{
		ID:     pgUUID(projectID),
		TeamID: pgUUID(teamID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("services: get project: %w", err)
	}
	return projectID, nil
}

// HasActiveDeploy implements Repository.
func (r *storeRepository) HasActiveDeploy(ctx context.Context, serviceID uuid.UUID) (bool, error) {
	active, err := r.store.HasActiveServiceDeploy(ctx, pgUUID(serviceID))
	if err != nil {
		return false, fmt.Errorf("services: active deploy check: %w", err)
	}
	return active, nil
}

// enrich fills the environment, project and server names of listed services.
// Rows of one listing share environments, so each distinct parent is read
// once.
func (r *storeRepository) enrich(ctx context.Context, services []Service) ([]Service, error) {
	envs := make(map[uuid.UUID]sqlc.Environment)
	servers := make(map[uuid.UUID]string)
	for i, service := range services {
		if service.EnvironmentID != uuid.Nil {
			env, ok := envs[service.EnvironmentID]
			if !ok {
				fetched, err := r.store.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
					ID:     pgUUID(service.EnvironmentID),
					TeamID: pgUUID(service.TeamID),
				})
				if err != nil {
					return nil, fmt.Errorf("services: get environment: %w", err)
				}
				env, envs[service.EnvironmentID] = fetched, fetched
			}
			services[i].EnvironmentName = env.Name
			services[i].ProjectID = uuidFromPG(env.ProjectID)
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

// UpdateServiceConfig implements Repository, mapping a missing row to
// ErrNotFound. Only the name, document and environment columns are written.
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

// UpdateServiceStatus implements Repository, mapping a missing row to
// ErrNotFound. Only the status column is written.
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

// SoftDeleteService implements Repository. A row that is already deleted
// reports ErrNotFound, which makes the delete idempotent for its caller.
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

// CreateServiceDeploy implements Repository.
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

// UpdateServiceDeploy implements Repository, mapping a missing row to
// ErrNotFound.
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

// ListServiceDeploys implements Repository.
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

// ServerExists implements Repository using the node registry table and the
// caller's active team.
func (r *storeRepository) ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error) {
	if serverID == uuid.Nil {
		return false, nil
	}
	row, err := r.store.GetServerByID(ctx, pgUUID(serverID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("services: resolve server: %w", err)
	}
	if err := scope.AuthorizeOptionalTeam(uuidFromPG(row.TeamID), true); err != nil {
		return false, nil
	}
	return true, nil
}

// serviceFromRow maps one sqlc row onto the domain type.
func serviceFromRow(row sqlc.Service) (Service, error) {
	env, err := unmarshalEnv(row.Env)
	if err != nil {
		return Service{}, err
	}
	return Service{
		ID:            uuidFromPG(row.ID),
		UserID:        uuidFromPG(row.UserID),
		TeamID:        uuidFromPG(row.TeamID),
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

// deployFromRow maps one sqlc deploy row onto the domain type.
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

// marshalEnv encodes the substitution environment for the jsonb column.
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

// unmarshalEnv decodes the jsonb column.
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

// uuidFromPG converts a pgx UUID to uuid.UUID.
func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

// pgUUID converts a uuid.UUID to the pgx type (invalid for uuid.Nil).
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// pgTime converts a time.Time to the pgx type (invalid for the zero time).
func pgTime(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value, Valid: true}
}

// timeFromPG converts a pgx timestamp to time.Time (zero when NULL).
func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

// isUniqueViolation reports whether err is a PostgreSQL unique-index conflict.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation reports a PostgreSQL foreign-key violation (SQLSTATE
// 23503): an environment deleted between validation and write.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
