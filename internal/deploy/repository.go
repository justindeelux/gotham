package deploy

import (
	"context"
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

// Repository persists applications, deployments and the application
// configuration the runtime payload is built from. It is implemented over
// *store.Store (sqlc) in production and by fakes in tests.
type Repository interface {
	// GetApplication returns the application, or ErrNotFound.
	GetApplication(ctx context.Context, appID uuid.UUID) (Application, error)
	// CreateDeployment stores a new deployment row.
	CreateDeployment(ctx context.Context, dep Deployment) (Deployment, error)
	// GetDeployment returns one deployment of an application, or ErrNotFound.
	GetDeployment(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error)
	// ListDeployments returns an application's deployments, newest first.
	ListDeployments(ctx context.Context, appID uuid.UUID) ([]Deployment, error)
	// FailStaleDeployments marks deployments left non-terminal by a previous
	// control plane process as failed and reports how many were recovered.
	FailStaleDeployments(ctx context.Context) (int64, error)
	// UpdateDeployment persists the mutable deployment fields.
	UpdateDeployment(ctx context.Context, dep Deployment) (Deployment, error)
	// ListEnvVars returns the application's plain environment variables.
	ListEnvVars(ctx context.Context, appID uuid.UUID) ([]EnvVar, error)
	// ListSecrets returns the application's sealed secrets.
	ListSecrets(ctx context.Context, appID uuid.UUID) ([]Secret, error)
	// ListStorages returns the application's volume map.
	ListStorages(ctx context.Context, appID uuid.UUID) ([]Storage, error)
}

// storeRepository adapts *store.Store to Repository.
type storeRepository struct {
	store *store.Store
}

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

// GetApplication loads one application, mapping a missing row to ErrNotFound.
func (r *storeRepository) GetApplication(ctx context.Context, appID uuid.UUID) (Application, error) {
	row, err := r.store.GetApplication(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, fmt.Errorf("deploy: get application: %w", err)
	}
	return applicationFromRow(row), nil
}

// CreateDeployment stores a queued deployment. The partial unique index on
// active deployments turns a concurrent submit into ErrConflict.
func (r *storeRepository) CreateDeployment(ctx context.Context, dep Deployment) (Deployment, error) {
	row, err := r.store.CreateDeployment(ctx, sqlc.CreateDeploymentParams{
		ApplicationID: pgUUID(dep.ApplicationID),
		Kind:          string(dep.Kind),
		State:         string(dep.State),
		ImageTag:      dep.ImageTag,
		RegistryImage: dep.RegistryImage,
		Digest:        dep.Digest,
		RollbackFrom:  pgUUID(dep.RollbackFrom),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Deployment{}, ErrConflict
		}
		return Deployment{}, fmt.Errorf("deploy: create deployment: %w", err)
	}
	return deploymentFromRow(row), nil
}

// GetDeployment loads one deployment of an application.
func (r *storeRepository) GetDeployment(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error) {
	row, err := r.store.GetDeployment(ctx, pgUUID(deploymentID), pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, fmt.Errorf("deploy: get deployment: %w", err)
	}
	return deploymentFromRow(row), nil
}

// ListDeployments loads every deployment of an application, newest first.
func (r *storeRepository) ListDeployments(ctx context.Context, appID uuid.UUID) ([]Deployment, error) {
	rows, err := r.store.ListDeploymentsByApp(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("deploy: list deployments: %w", err)
	}
	deployments := make([]Deployment, 0, len(rows))
	for _, row := range rows {
		deployments = append(deployments, deploymentFromRow(row))
	}
	return deployments, nil
}

// FailStaleDeployments marks deployments a previous control plane process
// left in a non-terminal state as failed, unblocking the active-deployment
// partial unique index for every application.
func (r *storeRepository) FailStaleDeployments(ctx context.Context) (int64, error) {
	n, err := r.store.FailStaleDeployments(ctx)
	if err != nil {
		return 0, fmt.Errorf("deploy: fail stale deployments: %w", err)
	}
	return n, nil
}

// UpdateDeployment persists the mutable deployment fields and returns the row.
func (r *storeRepository) UpdateDeployment(ctx context.Context, dep Deployment) (Deployment, error) {
	row, err := r.store.UpdateDeployment(ctx, sqlc.UpdateDeploymentParams{
		ID:            pgUUID(dep.ID),
		State:         string(dep.State),
		ImageTag:      dep.ImageTag,
		RegistryImage: dep.RegistryImage,
		Digest:        dep.Digest,
		Error:         dep.Error,
		Attempt:       dep.Attempt,
		ContainerID:   dep.ContainerID,
		StartedAt:     pgTime(dep.StartedAt),
		FinishedAt:    pgTime(dep.FinishedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, fmt.Errorf("deploy: update deployment: %w", err)
	}
	return deploymentFromRow(row), nil
}

// ListEnvVars loads the application's plain environment variables.
func (r *storeRepository) ListEnvVars(ctx context.Context, appID uuid.UUID) ([]EnvVar, error) {
	rows, err := r.store.ListEnvVarsByApp(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("deploy: list env vars: %w", err)
	}
	vars := make([]EnvVar, 0, len(rows))
	for _, row := range rows {
		vars = append(vars, EnvVar{
			ID:            uuidFromPG(row.ID),
			ApplicationID: uuidFromPG(row.ApplicationID),
			Key:           row.Key,
			Value:         row.Value,
			CreatedAt:     timeFromPG(row.CreatedAt),
		})
	}
	return vars, nil
}

// ListSecrets loads the application's sealed secrets (ciphertext untouched).
func (r *storeRepository) ListSecrets(ctx context.Context, appID uuid.UUID) ([]Secret, error) {
	rows, err := r.store.ListSecretsByApp(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("deploy: list secrets: %w", err)
	}
	secrets := make([]Secret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, Secret{
			ID:            uuidFromPG(row.ID),
			ApplicationID: uuidFromPG(row.ApplicationID),
			Key:           row.Key,
			Ciphertext:    row.Ciphertext,
			CreatedAt:     timeFromPG(row.CreatedAt),
		})
	}
	return secrets, nil
}

// ListStorages loads the application's volume map.
func (r *storeRepository) ListStorages(ctx context.Context, appID uuid.UUID) ([]Storage, error) {
	rows, err := r.store.ListStoragesByApp(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("deploy: list storages: %w", err)
	}
	storages := make([]Storage, 0, len(rows))
	for _, row := range rows {
		storages = append(storages, Storage{
			ID:            uuidFromPG(row.ID),
			ApplicationID: uuidFromPG(row.ApplicationID),
			Name:          row.Name,
			HostPath:      row.HostPath,
			ContainerPath: row.ContainerPath,
			CreatedAt:     timeFromPG(row.CreatedAt),
		})
	}
	return storages, nil
}

// applicationFromRow maps a sqlc row to the domain model. A NULL server_id
// becomes the zero UUID, which validation rejects at enqueue time.
func applicationFromRow(row sqlc.Application) Application {
	return Application{
		ID:         uuidFromPG(row.ID),
		UserID:     uuidFromPG(row.UserID),
		ServerID:   uuidFromPG(row.ServerID),
		Name:       row.Name,
		Provider:   row.Provider,
		Repo:       row.Repo,
		CloneURL:   row.CloneUrl,
		Branch:     row.Branch,
		BuildPack:  row.BuildPack,
		BaseDomain: row.BaseDomain,
		Port:       row.Port,
		HostPort:   row.HostPort,
	}
}

// deploymentFromRow maps a sqlc row to the domain model.
func deploymentFromRow(row sqlc.Deployment) Deployment {
	return Deployment{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		Kind:          Kind(row.Kind),
		State:         State(row.State),
		ImageTag:      row.ImageTag,
		RegistryImage: row.RegistryImage,
		Digest:        row.Digest,
		Error:         row.Error,
		Attempt:       row.Attempt,
		ContainerID:   row.ContainerID,
		RollbackFrom:  uuidFromPG(row.RollbackFrom),
		StartedAt:     timeFromPG(row.StartedAt),
		FinishedAt:    timeFromPG(row.FinishedAt),
		CreatedAt:     timeFromPG(row.CreatedAt),
		UpdatedAt:     timeFromPG(row.UpdatedAt),
	}
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), which the active-deployment index raises.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// pgUUID converts a domain UUID for sqlc. The zero UUID becomes an invalid
// pgtype value, which serialises as NULL for nullable columns.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a sqlc UUID column to the domain type (NULL → zero).
func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

// pgTime converts a domain timestamp for sqlc (zero → NULL).
func pgTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// timeFromPG converts a sqlc timestamp column to the domain type.
func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}
