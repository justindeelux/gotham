package deploy

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

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

// Repository persists applications, deployments and the application
// configuration the runtime payload is built from. It is implemented over
// *store.Store (sqlc) in production and by fakes in tests.
type Repository interface {
	// GetApplication returns the application, or ErrNotFound.
	GetApplication(ctx context.Context, appID uuid.UUID) (Application, error)
	// ListApplications returns the applications of the scope's active team
	// (or, without a team context, of the creator), newest first.
	ListApplications(ctx context.Context, scope teams.Scope) ([]Application, error)
	// ListApplicationsByEnvironment returns one environment's applications,
	// newest first (previews only when includePreviews is true).
	ListApplicationsByEnvironment(ctx context.Context, environmentID uuid.UUID, includePreviews bool) ([]Application, error)
	// ListApplicationsByProject returns every environment's applications of
	// one project, newest first (previews only when includePreviews is true).
	ListApplicationsByProject(ctx context.Context, projectID uuid.UUID, includePreviews bool) ([]Application, error)
	// NameInEnvironment reports whether the environment holds another
	// application with name (the move-collision pre-check).
	NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error)
	// ResolveEnvironment validates that environmentID belongs to teamID and
	// returns it (ErrNotFound for a foreign or missing one).
	ResolveEnvironment(ctx context.Context, environmentID, teamID uuid.UUID) (EnvironmentRef, error)
	// ResolveProject validates that projectID belongs to teamID
	// (ErrNotFound for a foreign or missing one).
	ResolveProject(ctx context.Context, projectID, teamID uuid.UUID) (uuid.UUID, error)
	// HasLivePreviews reports whether the base application has live
	// (non-deleted) preview bindings, so a move or server change can be
	// refused while previews are open.
	HasLivePreviews(ctx context.Context, appID uuid.UUID) (bool, error)
	// CreateApplication stores a new application together with its env vars,
	// sealed secrets and storages in one transaction.
	CreateApplication(ctx context.Context, app Application, envVars []EnvVar, secrets []Secret, storages []Storage) (Application, error)
	// UpdateApplication persists the mutable application fields.
	UpdateApplication(ctx context.Context, app Application) (Application, error)
	// DeleteApplication removes the application row; its configuration
	// (env vars, secrets, storages, deployments) cascades with it.
	DeleteApplication(ctx context.Context, appID uuid.UUID) error
	// ReplaceEnvVars replaces the application's plain env vars and sealed
	// secrets as one set.
	ReplaceEnvVars(ctx context.Context, appID uuid.UUID, envVars []EnvVar, secrets []Secret) error
	// ReplaceStorages replaces the application's storage mappings as one set.
	ReplaceStorages(ctx context.Context, appID uuid.UUID, storages []Storage) error
	// ServerExists reports whether the target server is registered AND
	// actionable by the caller's active team. A node of another team answers
	// false, like a missing one, so node IDs cannot be probed (F6); a legacy
	// node without a team stays shared.
	ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error)
	// ServerTeam reports the team a registered node belongs to (uuid.Nil for a
	// legacy shared node) and whether it exists. It backs the stored
	// application→node invariant checked at the queue boundary.
	ServerTeam(ctx context.Context, serverID uuid.UUID) (uuid.UUID, bool, error)
	// GitHubAppOwnedBy reports whether the GitHub App connection belongs to
	// the caller. A foreign id answers false, like a missing one, so
	// connection IDs cannot be probed.
	GitHubAppOwnedBy(ctx context.Context, id, userID uuid.UUID) (bool, error)
	// CreateDeployment stores a new deployment row.
	CreateDeployment(ctx context.Context, dep Deployment) (Deployment, error)
	// GetDeployment returns one deployment of an application, or ErrNotFound.
	GetDeployment(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error)
	// ListDeployments returns an application's deployments, newest first.
	ListDeployments(ctx context.Context, appID uuid.UUID) ([]Deployment, error)
	// ListDeploymentsLimit returns at most limit of an application's newest
	// deployments, so readers that only need the latest state (the dashboard
	// running count) never pull the full history.
	ListDeploymentsLimit(ctx context.Context, appID uuid.UUID, limit int) ([]Deployment, error)
	// FailStaleDeployments marks deployments left non-terminal by a previous
	// control plane process as failed and reports how many were recovered.
	FailStaleDeployments(ctx context.Context) (int64, error)
	// UpdateDeployment persists the mutable deployment fields.
	UpdateDeployment(ctx context.Context, dep Deployment) (Deployment, error)
	// ListEnvVars returns the application's plain environment variables.
	ListEnvVars(ctx context.Context, appID uuid.UUID) ([]EnvVar, error)
	// ListSecrets returns the application's sealed secrets.
	ListSecrets(ctx context.Context, appID uuid.UUID) ([]Secret, error)
	// ListEnvConfig returns the application's plain env vars and sealed
	// secrets from one snapshot, so a replace committing between them cannot
	// drop a key from either collection. The runtime payload is assembled from
	// it.
	ListEnvConfig(ctx context.Context, appID uuid.UUID) ([]EnvVar, []Secret, error)
	// ListStorages returns the application's volume map.
	ListStorages(ctx context.Context, appID uuid.UUID) ([]Storage, error)
	// GetDeployKey returns the deploy key of an application, or ErrNotFound.
	GetDeployKey(ctx context.Context, appID uuid.UUID) (DeployKey, error)
	// CreateDeployKey stores the mapping row together with the private key it
	// points at, sealed with providers.SealSecret (the private_keys contract).
	CreateDeployKey(ctx context.Context, key DeployKey, privateKeyPEM string) (DeployKey, error)
	// DeleteDeployKey removes the deploy key the caller read: the mapping row
	// and the private key it points at. The delete is fenced on key.ID, so a
	// stale concurrent delete cannot remove a replacement key created after the
	// read. It returns the removed mapping, or ErrNotFound when the fenced row
	// was already gone.
	DeleteDeployKey(ctx context.Context, key DeployKey) (DeployKey, error)
	// DeletePrivateKey removes one sealed private key row by ID (its FK
	// cascades the deploy-key mapping). It is the orphan-cleanup fallback when
	// the fenced mapping delete fails before the application cascade would
	// strand the sealed key.
	DeletePrivateKey(ctx context.Context, privateKeyID uuid.UUID) error
	// DeployKeyPrivatePEM opens an application's deploy private key for the
	// cloner. An application without a key answers "" and no error, which is
	// what keeps anonymous cloning the default.
	DeployKeyPrivatePEM(ctx context.Context, appID uuid.UUID) (string, error)
	// UpsertGitCredential stores (or rotates) an application's sealed HTTPS
	// token for git_private sources (GS-4). The token arrives already sealed
	// with providers.SealSecret.
	UpsertGitCredential(ctx context.Context, appID uuid.UUID, username, sealedToken string) error
	// GetGitCredential returns an application's HTTPS credential metadata, or
	// ErrNotFound when none is set. The sealed token never leaves the store
	// through this method.
	GetGitCredential(ctx context.Context, appID uuid.UUID) (GitCredential, error)
	// GitCredential opens an application's HTTPS git credential for the
	// cloner and the connection test. An application without one answers ""
	// and no error, which is what keeps anonymous cloning the default.
	GitCredential(ctx context.Context, appID uuid.UUID) (username, token string, err error)
	// DeleteGitCredential removes an application's HTTPS credential row. It
	// answers ErrNotFound when none is set, so the service can report the
	// idempotent outcome honestly.
	DeleteGitCredential(ctx context.Context, appID uuid.UUID) error
	// CopyGitCredential copies the sealed HTTPS credential row to a preview
	// sibling (the preview inherits the token like it inherits the deploy
	// key). An application without one is a success with nothing copied.
	CopyGitCredential(ctx context.Context, baseID, previewID uuid.UUID) error
	// GetCertificateIntent returns an application's certificate configuration,
	// or ErrNotFound when it has none. The preview clone reads it to copy an
	// enabled wildcard DNS-01 intent onto the sibling.
	GetCertificateIntent(ctx context.Context, appID uuid.UUID) (CertificateIntent, error)
	// GetDNSProviderInfo returns the zones and usability of one DNS provider,
	// or ErrNotFound (the certificate FK keeps a referenced row alive).
	GetDNSProviderInfo(ctx context.Context, providerID uuid.UUID) (DNSProviderInfo, error)
	// ListSharedVariables returns one application's project-level variables
	// and its environment's variables (in that order) from one snapshot, so
	// the deploy merge reads both scopes without a concurrent replace
	// slipping between them. Secrets stay sealed; the payload opens them.
	ListSharedVariables(ctx context.Context, projectID, environmentID uuid.UUID) (project []SharedVariable, environment []SharedVariable, err error)
	// CreateCertificateIntent stores one application's certificate
	// configuration. The per-application unique index is a conflict.
	CreateCertificateIntent(ctx context.Context, in CertificateIntent) error
}

// storeRepository adapts *store.Store to Repository. secret opens sealed
// values (application secrets, deploy private keys) with providers.SealSecret.
type storeRepository struct {
	store  *store.Store
	secret string
}

// newStoreRepository builds the PostgreSQL-backed repository. secret is the
// key providers.SealSecret seals values with; the same key must be configured
// at read time, so it comes from the service Config either way.
func newStoreRepository(st *store.Store, secret string) *storeRepository {
	return &storeRepository{store: st, secret: secret}
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
	enriched, err := r.enrich(ctx, []sqlc.Application{row})
	if err != nil {
		return Application{}, err
	}
	return enriched[0], nil
}

// ListApplications loads the active team's applications, or the creator's when
// no team context is present, newest first.
func (r *storeRepository) ListApplications(ctx context.Context, scope teams.Scope) ([]Application, error) {
	var (
		rows []sqlc.Application
		err  error
	)
	if scope.Active() {
		rows, err = r.store.ListApplicationsByTeam(ctx, pgUUID(scope.TeamID))
	} else {
		rows, err = r.store.ListApplicationsByUser(ctx, pgUUID(scope.UserID))
	}
	if err != nil {
		return nil, fmt.Errorf("deploy: list applications: %w", err)
	}
	return r.enrich(ctx, rows)
}

// ListApplicationsByEnvironment implements Repository.
func (r *storeRepository) ListApplicationsByEnvironment(ctx context.Context, environmentID uuid.UUID, includePreviews bool) ([]Application, error) {
	rows, err := r.store.ListApplicationsByEnvironment(ctx, sqlc.ListApplicationsByEnvironmentParams{
		EnvironmentID:   pgUUID(environmentID),
		IncludePreviews: includePreviews,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy: list applications by environment: %w", err)
	}
	return r.enrich(ctx, rows)
}

// ListApplicationsByProject implements Repository.
func (r *storeRepository) ListApplicationsByProject(ctx context.Context, projectID uuid.UUID, includePreviews bool) ([]Application, error) {
	rows, err := r.store.ListApplicationsByProject(ctx, sqlc.ListApplicationsByProjectParams{
		ProjectID:       pgUUID(projectID),
		IncludePreviews: includePreviews,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy: list applications by project: %w", err)
	}
	return r.enrich(ctx, rows)
}

// NameInEnvironment implements Repository: the move-collision pre-check. The
// comparison is exact, matching the (environment_id, name) unique index.
func (r *storeRepository) NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error) {
	collision, err := r.store.ApplicationNameInEnvironment(ctx, sqlc.ApplicationNameInEnvironmentParams{
		EnvironmentID: pgUUID(environmentID),
		Name:          name,
		ID:            pgUUID(exceptID),
	})
	if err != nil {
		return false, fmt.Errorf("deploy: name in environment: %w", err)
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
		return EnvironmentRef{}, fmt.Errorf("deploy: get environment: %w", err)
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
		return uuid.Nil, fmt.Errorf("deploy: get project: %w", err)
	}
	return projectID, nil
}

// HasLivePreviews implements Repository.
func (r *storeRepository) HasLivePreviews(ctx context.Context, appID uuid.UUID) (bool, error) {
	live, err := r.store.HasLivePreviewBindings(ctx, pgUUID(appID))
	if err != nil {
		return false, fmt.Errorf("deploy: live preview check: %w", err)
	}
	return live, nil
}

// enrich maps rows to the domain model and fills the environment, project
// and server names from the contract. Rows of one listing share environments,
// so each distinct parent is read once.
func (r *storeRepository) enrich(ctx context.Context, rows []sqlc.Application) ([]Application, error) {
	applications := make([]Application, 0, len(rows))
	envs := make(map[uuid.UUID]sqlc.Environment)
	projects := make(map[uuid.UUID]sqlc.Project)
	servers := make(map[uuid.UUID]string)
	for _, row := range rows {
		app := applicationFromRow(row)
		envID := uuidFromPG(row.EnvironmentID)
		if envID != uuid.Nil {
			env, ok := envs[envID]
			if !ok {
				fetched, err := r.store.GetEnvironment(ctx, sqlc.GetEnvironmentParams{
					ID:     pgUUID(envID),
					TeamID: pgUUID(app.TeamID),
				})
				if err != nil {
					return nil, fmt.Errorf("deploy: get environment: %w", err)
				}
				env, envs[envID] = fetched, fetched
			}
			app.EnvironmentName = env.Name
			projectID := uuidFromPG(env.ProjectID)
			app.ProjectID = projectID
			if projectID != uuid.Nil {
				project, ok := projects[projectID]
				if !ok {
					fetched, err := r.store.GetProject(ctx, sqlc.GetProjectParams{
						ID:     pgUUID(projectID),
						TeamID: pgUUID(app.TeamID),
					})
					if err != nil {
						return nil, fmt.Errorf("deploy: get project: %w", err)
					}
					project, projects[projectID] = fetched, fetched
				}
				app.ProjectName = project.Name
			}
		}
		if app.ServerID != uuid.Nil {
			name, ok := servers[app.ServerID]
			if !ok {
				server, err := r.store.GetServerByID(ctx, pgUUID(app.ServerID))
				if err != nil {
					return nil, fmt.Errorf("deploy: get server: %w", err)
				}
				name, servers[app.ServerID] = server.Name, server.Name
			}
			app.ServerName = name
		}
		applications = append(applications, app)
	}
	return applications, nil
}

// CreateApplication stores the application row and its configuration in a
// single transaction, so a rejected child row (a duplicate key, a bad storage
// name) never leaves an application without its settings behind.
func (r *storeRepository) CreateApplication(
	ctx context.Context,
	app Application,
	envVars []EnvVar,
	secrets []Secret,
	storages []Storage,
) (Application, error) {
	row, err := r.store.CreateApplicationWithConfig(ctx,
		sqlc.CreateApplicationParams{
			ID:                         pgUUID(app.ID),
			UserID:                     pgUUID(app.UserID),
			TeamID:                     pgUUID(app.TeamID),
			ServerID:                   pgUUID(app.ServerID),
			EnvironmentID:              pgUUID(app.EnvironmentID),
			Name:                       app.Name,
			Provider:                   app.Provider,
			Repo:                       app.Repo,
			CloneUrl:                   app.CloneURL,
			Branch:                     app.Branch,
			BuildPack:                  app.BuildPack,
			BaseDomain:                 app.BaseDomain,
			SourceType:                 NormalizeSourceType(app.SourceType, app.Provider),
			Port:                       app.Port,
			HostPort:                   app.HostPort,
			IsPreview:                  app.IsPreview,
			GithubAppID:                pgUUID(app.GitHubAppID),
			DockerfileContent:          app.DockerfileContent,
			BuildArgs:                  marshalBuildArgs(app.BuildArgs),
			ImageRef:                   app.ImageRef,
			RegistryUsername:           app.RegistryUsername,
			RegistryPasswordCiphertext: app.RegistryPasswordCiphertext,
			ComposeContent:             app.ComposeContent,
			ComposeFile:                app.ComposeFile,
			ComposeService:             app.ComposeService,
		},
		envVarParams(envVars),
		secretParams(secrets),
		storageParams(storages),
	)
	if err != nil {
		return Application{}, applicationWriteError(err, app.Name)
	}
	enriched, err := r.enrich(ctx, []sqlc.Application{row})
	if err != nil {
		return Application{}, err
	}
	return enriched[0], nil
}

// UpdateApplication persists the mutable application fields and returns the row.
func (r *storeRepository) UpdateApplication(ctx context.Context, app Application) (Application, error) {
	row, err := r.store.UpdateApplication(ctx, sqlc.UpdateApplicationParams{
		ID:                         pgUUID(app.ID),
		Name:                       app.Name,
		Branch:                     app.Branch,
		BuildPack:                  app.BuildPack,
		BaseDomain:                 app.BaseDomain,
		Port:                       app.Port,
		HostPort:                   app.HostPort,
		ServerID:                   pgUUID(app.ServerID),
		BaseDomainDisabled:         app.BaseDomainDisabled,
		EnvironmentID:              pgUUID(app.EnvironmentID),
		GithubAppID:                pgUUID(app.GitHubAppID),
		DockerfileContent:          app.DockerfileContent,
		BuildArgs:                  marshalBuildArgs(app.BuildArgs),
		ImageRef:                   app.ImageRef,
		RegistryUsername:           app.RegistryUsername,
		RegistryPasswordCiphertext: app.RegistryPasswordCiphertext,
		ComposeContent:             app.ComposeContent,
		ComposeFile:                app.ComposeFile,
		ComposeService:             app.ComposeService,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, applicationWriteError(err, app.Name)
	}
	enriched, err := r.enrich(ctx, []sqlc.Application{row})
	if err != nil {
		return Application{}, err
	}
	return enriched[0], nil
}

// DeleteApplication removes the application row (children cascade).
func (r *storeRepository) DeleteApplication(ctx context.Context, appID uuid.UUID) error {
	if err := r.store.DeleteApplication(ctx, pgUUID(appID)); err != nil {
		return fmt.Errorf("deploy: delete application: %w", err)
	}
	return nil
}

// ReplaceEnvVars rewrites the plain env vars and sealed secrets of an
// application as one set, inside a single transaction.
func (r *storeRepository) ReplaceEnvVars(ctx context.Context, appID uuid.UUID, envVars []EnvVar, secrets []Secret) error {
	if err := r.store.ReplaceApplicationEnv(ctx, pgUUID(appID), envVarParams(envVars), secretParams(secrets)); err != nil {
		return fmt.Errorf("deploy: replace env vars: %w", err)
	}
	return nil
}

// ReplaceStorages rewrites the storage mappings of an application as one set,
// inside a single transaction.
func (r *storeRepository) ReplaceStorages(ctx context.Context, appID uuid.UUID, storages []Storage) error {
	if err := r.store.ReplaceApplicationStorages(ctx, pgUUID(appID), storageParams(storages)); err != nil {
		return fmt.Errorf("deploy: replace storages: %w", err)
	}
	return nil
}

// ServerExists reports whether the server is registered on the control plane
// and may be targeted by the caller's active team.
func (r *storeRepository) ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error) {
	if serverID == uuid.Nil {
		return false, nil
	}
	row, err := r.store.GetServerByID(ctx, pgUUID(serverID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("deploy: get server: %w", err)
	}
	if err := scope.AuthorizeOptionalTeam(uuidFromPG(row.TeamID), true); err != nil {
		return false, nil
	}
	return true, nil
}

// ServerTeam reports the team of a registered node (the zero UUID for a legacy
// shared node) and whether it exists at all.
func (r *storeRepository) ServerTeam(ctx context.Context, serverID uuid.UUID) (uuid.UUID, bool, error) {
	if serverID == uuid.Nil {
		return uuid.Nil, false, nil
	}
	row, err := r.store.GetServerByID(ctx, pgUUID(serverID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("deploy: get server team: %w", err)
	}
	return uuidFromPG(row.TeamID), true, nil
}

// GitHubAppOwnedBy reports whether the GitHub App connection belongs to the
// caller. A foreign or missing id answers false without distinguishing the
// two, so connection IDs cannot be probed.
func (r *storeRepository) GitHubAppOwnedBy(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	if id == uuid.Nil {
		return false, nil
	}
	_, err := r.store.GetGitHubAppByIDAndUser(ctx, sqlc.GetGitHubAppByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("deploy: github app owner: %w", err)
	}
	return true, nil
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
		// The compose document travels only for compose rollbacks (see
		// Rollback); every other deployment stores empty values through the
		// COALESCE defaults. The commit metadata travels the same way: a
		// fresh clone records it, a rollback copies the target's.
		ComposeDocument: dep.ComposeDocument,
		ComposeCommit:   dep.ComposeCommit,
		CommitSha:       dep.CommitSHA,
		CommitMessage:   dep.CommitMessage,
		CommitAuthor:    dep.CommitAuthor,
		CommittedAt:     dep.CommittedAt,
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

// ListDeploymentsLimit loads at most limit deployments of an application,
// newest first. A non-positive limit selects one row: the route rejects
// ?limit=<1 with a 400 before this runs, so the guard only covers
// in-process callers.
func (r *storeRepository) ListDeploymentsLimit(ctx context.Context, appID uuid.UUID, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.store.ListDeploymentsByAppLimit(ctx, pgUUID(appID), int32(limit))
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
		// The build step records the rendered compose document of a
		// compose run here, so a rollback re-applies this release's file.
		ComposeDocument: dep.ComposeDocument,
		ComposeCommit:   dep.ComposeCommit,
		// The clone step records the commit it checked out here.
		CommitSha:     dep.CommitSHA,
		CommitMessage: dep.CommitMessage,
		CommitAuthor:  dep.CommitAuthor,
		CommittedAt:   dep.CommittedAt,
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

// ListSharedVariables implements Repository: the project-level rows and one
// environment's rows from one snapshot, split by scope so the merge can layer
// them (project < environment). Secrets stay sealed; buildEnv opens every
// ciphertext when the payload is assembled. A zero project or environment id
// fails explicitly instead of silently matching no rows.
func (r *storeRepository) ListSharedVariables(ctx context.Context, projectID, environmentID uuid.UUID) ([]SharedVariable, []SharedVariable, error) {
	if projectID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: application has no project assigned", ErrValidation)
	}
	if environmentID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: application has no environment assigned", ErrValidation)
	}
	rows, err := r.store.ListSharedVariablesForEnvironment(ctx, pgUUID(projectID), pgUUID(environmentID))
	if err != nil {
		return nil, nil, fmt.Errorf("deploy: list shared variables: %w", err)
	}
	var project, environment []SharedVariable
	for _, row := range rows {
		variable := SharedVariable{
			Key:        row.Key,
			Value:      row.Value,
			Ciphertext: row.Ciphertext,
			Secret:     row.Secret,
		}
		if uuidFromPG(row.EnvironmentID) == uuid.Nil {
			project = append(project, variable)
		} else {
			environment = append(environment, variable)
		}
	}
	return project, environment, nil
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

// ListEnvConfig loads the plain env vars and sealed secrets of an application
// from one database snapshot, so a concurrent replace cannot leave a key in
// neither collection (see store.ListEnvConfigByApp).
func (r *storeRepository) ListEnvConfig(ctx context.Context, appID uuid.UUID) ([]EnvVar, []Secret, error) {
	envRows, secretRows, err := r.store.ListEnvConfigByApp(ctx, pgUUID(appID))
	if err != nil {
		return nil, nil, fmt.Errorf("deploy: list env config: %w", err)
	}
	vars := make([]EnvVar, 0, len(envRows))
	for _, row := range envRows {
		vars = append(vars, EnvVar{
			ID:            uuidFromPG(row.ID),
			ApplicationID: uuidFromPG(row.ApplicationID),
			Key:           row.Key,
			Value:         row.Value,
			CreatedAt:     timeFromPG(row.CreatedAt),
		})
	}
	secrets := make([]Secret, 0, len(secretRows))
	for _, row := range secretRows {
		secrets = append(secrets, Secret{
			ID:            uuidFromPG(row.ID),
			ApplicationID: uuidFromPG(row.ApplicationID),
			Key:           row.Key,
			Ciphertext:    row.Ciphertext,
			CreatedAt:     timeFromPG(row.CreatedAt),
		})
	}
	return vars, secrets, nil
}

// GetDeployKey loads the deploy key of an application, or ErrNotFound.
func (r *storeRepository) GetDeployKey(ctx context.Context, appID uuid.UUID) (DeployKey, error) {
	row, err := r.store.GetApplicationDeployKey(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeployKey{}, ErrNotFound
		}
		return DeployKey{}, fmt.Errorf("deploy: get deploy key: %w", err)
	}
	return deployKeyFromRow(row), nil
}

// CreateDeployKey stores the deploy key of an application, sealing the private
// half with providers.SealSecret — the same base64(nonce||ciphertext) AES-256
// GCM shape servers.EncryptKey writes into private_keys.encrypted_key, so both
// readers open the same row format.
func (r *storeRepository) CreateDeployKey(ctx context.Context, key DeployKey, privateKeyPEM string) (DeployKey, error) {
	sealed, err := providers.SealSecret(r.secret, privateKeyPEM)
	if err != nil {
		return DeployKey{}, fmt.Errorf("deploy: seal deploy key: %w", err)
	}
	row, err := r.store.CreateApplicationDeployKey(ctx, sqlc.CreateApplicationDeployKeyParams{
		ApplicationID: pgUUID(key.ApplicationID),
		Provider:      key.Provider,
		Repo:          key.Repo,
		ProviderKeyID: key.ProviderKeyID,
		Fingerprint:   key.Fingerprint,
		PublicKey:     key.PublicKey,
	}, deployKeyRowName(key.ApplicationID), sealed)
	if err != nil {
		if isUniqueViolation(err) {
			return DeployKey{}, ErrConflict
		}
		return DeployKey{}, fmt.Errorf("deploy: create deploy key: %w", err)
	}
	return deployKeyFromRow(row), nil
}

// DeleteDeployKey removes the deploy key the caller read, fenced on its mapping
// ID. A stale delete whose row was replaced matches nothing and answers
// ErrNotFound, leaving the replacement intact.
func (r *storeRepository) DeleteDeployKey(ctx context.Context, key DeployKey) (DeployKey, error) {
	row, err := r.store.DeleteApplicationDeployKey(ctx, pgUUID(key.ID), pgUUID(key.ApplicationID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeployKey{}, ErrNotFound
		}
		return DeployKey{}, fmt.Errorf("deploy: delete deploy key: %w", err)
	}
	return deployKeyFromRow(row), nil
}

// DeletePrivateKey removes one sealed private key row by ID. It is idempotent:
// an already-gone row is not an error.
func (r *storeRepository) DeletePrivateKey(ctx context.Context, privateKeyID uuid.UUID) error {
	if err := r.store.DeletePrivateKey(ctx, pgUUID(privateKeyID)); err != nil {
		return fmt.Errorf("deploy: delete private key: %w", err)
	}
	return nil
}

// DeployKeyPrivatePEM opens an application's deploy private key for the
// cloner. An application without a key answers "" and no error; a key that
// cannot be opened (rotated secret, corrupted row) is an error — falling back
// to an anonymous clone would hide the real problem behind an auth failure
// from the Git host.
func (r *storeRepository) DeployKeyPrivatePEM(ctx context.Context, appID uuid.UUID) (string, error) {
	mapping, err := r.store.GetApplicationDeployKey(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("deploy: get deploy key: %w", err)
	}
	row, err := r.store.GetPrivateKeyByID(ctx, mapping.PrivateKeyID)
	if err != nil {
		return "", fmt.Errorf("deploy: get deploy private key: %w", err)
	}
	privatePEM, err := providers.OpenSecret(r.secret, row.EncryptedKey)
	if err != nil {
		return "", fmt.Errorf("deploy: open deploy private key: %w", err)
	}
	return privatePEM, nil
}

// UpsertGitCredential implements Repository: it stores the sealed HTTPS
// token of a git_private application (sealed by the caller with
// providers.SealSecret, mirroring CreateDeployKey).
func (r *storeRepository) UpsertGitCredential(ctx context.Context, appID uuid.UUID, username, sealedToken string) error {
	if _, err := r.store.UpsertApplicationGitCredential(ctx, pgUUID(appID), username, sealedToken); err != nil {
		return fmt.Errorf("deploy: upsert git credential: %w", err)
	}
	return nil
}

// GetGitCredential implements Repository: the metadata callers may see. The
// ciphertext stays in the row — only the connection test and the cloner open
// it, through GitCredential below.
func (r *storeRepository) GetGitCredential(ctx context.Context, appID uuid.UUID) (GitCredential, error) {
	row, err := r.store.GetApplicationGitCredential(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GitCredential{}, ErrNotFound
		}
		return GitCredential{}, fmt.Errorf("deploy: get git credential: %w", err)
	}
	return GitCredential{Username: row.Username, UpdatedAt: timeFromPG(row.UpdatedAt)}, nil
}

// GitCredential implements Repository (and gitCredentialResolver): it opens
// an application's HTTPS token for the cloner and the connection test. An
// application without one answers empty strings and no error; a credential
// that cannot be opened is an error, never a silent anonymous fallback.
func (r *storeRepository) GitCredential(ctx context.Context, appID uuid.UUID) (string, string, error) {
	row, err := r.store.GetApplicationGitCredential(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("deploy: get git credential: %w", err)
	}
	token, err := providers.OpenSecret(r.secret, row.Ciphertext)
	if err != nil {
		return "", "", fmt.Errorf("deploy: open git credential: %w", err)
	}
	return row.Username, token, nil
}

// DeleteGitCredential implements Repository: it removes the row, answering
// ErrNotFound when none is set.
func (r *storeRepository) DeleteGitCredential(ctx context.Context, appID uuid.UUID) error {
	if _, err := r.store.GetApplicationGitCredential(ctx, pgUUID(appID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("deploy: get git credential: %w", err)
	}
	if err := r.store.DeleteApplicationGitCredential(ctx, pgUUID(appID)); err != nil {
		return fmt.Errorf("deploy: delete git credential: %w", err)
	}
	return nil
}

// CopyGitCredential implements Repository: the preview sibling inherits the
// sealed row as is (re-sealing would need the plaintext, which never leaves
// the sealed store). The delete cascade on the applications FK removes the
// copy with the sibling.
func (r *storeRepository) CopyGitCredential(ctx context.Context, baseID, previewID uuid.UUID) error {
	row, err := r.store.GetApplicationGitCredential(ctx, pgUUID(baseID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("deploy: get git credential: %w", err)
	}
	if _, err := r.store.UpsertApplicationGitCredential(ctx, pgUUID(previewID), row.Username, row.Ciphertext); err != nil {
		return fmt.Errorf("deploy: copy git credential: %w", err)
	}
	return nil
}

// GetCertificateIntent loads an application's certificate configuration, or
// ErrNotFound.
func (r *storeRepository) GetCertificateIntent(ctx context.Context, appID uuid.UUID) (CertificateIntent, error) {
	row, err := r.store.GetDomainCertificateByApplication(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CertificateIntent{}, ErrNotFound
		}
		return CertificateIntent{}, fmt.Errorf("deploy: get certificate intent: %w", err)
	}
	return CertificateIntent{
		ApplicationID: uuidFromPG(row.ApplicationID),
		Domain:        row.Domain,
		Enabled:       row.Enabled,
		Challenge:     row.Challenge,
		DNSProviderID: uuidFromPG(row.DnsProviderID),
		Wildcard:      row.Wildcard,
	}, nil
}

// GetDNSProviderInfo loads the zones and usability of one DNS provider, or
// ErrNotFound.
func (r *storeRepository) GetDNSProviderInfo(ctx context.Context, providerID uuid.UUID) (DNSProviderInfo, error) {
	row, err := r.store.GetDNSProvider(ctx, pgUUID(providerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DNSProviderInfo{}, ErrNotFound
		}
		return DNSProviderInfo{}, fmt.Errorf("deploy: get DNS provider: %w", err)
	}
	return DNSProviderInfo{Zones: append([]string{}, row.Zones...), Enabled: row.Enabled}, nil
}

// CreateCertificateIntent stores one application's certificate configuration.
// The one-certificate-per-application index surfaces as ErrConflict.
func (r *storeRepository) CreateCertificateIntent(ctx context.Context, in CertificateIntent) error {
	_, err := r.store.CreateDomainCertificate(ctx, sqlc.CreateDomainCertificateParams{
		ApplicationID: pgUUID(in.ApplicationID),
		Domain:        in.Domain,
		Enabled:       in.Enabled,
		Challenge:     in.Challenge,
		DnsProviderID: pgUUID(in.DNSProviderID),
		Wildcard:      in.Wildcard,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: the application already has a certificate configuration", ErrConflict)
		}
		return fmt.Errorf("deploy: create certificate intent: %w", err)
	}
	return nil
}

// marshalBuildArgs encodes the --build-arg pairs for the build_args jsonb
// column. An empty collection stores '{}', never NULL or JSON null.
func marshalBuildArgs(args map[string]string) []byte {
	if len(args) == 0 {
		return []byte("{}")
	}
	data, err := json.Marshal(args)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// unmarshalBuildArgs decodes the build_args jsonb column. Empty or corrupt
// values decode to nil, which the API renders as an empty object.
func unmarshalBuildArgs(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var args map[string]string
	if err := json.Unmarshal(data, &args); err != nil {
		return nil
	}
	return args
}

// applicationFromRow maps a sqlc row to the domain model.
func applicationFromRow(row sqlc.Application) Application {
	return Application{
		ID:                         uuidFromPG(row.ID),
		UserID:                     uuidFromPG(row.UserID),
		TeamID:                     uuidFromPG(row.TeamID),
		ServerID:                   uuidFromPG(row.ServerID),
		EnvironmentID:              uuidFromPG(row.EnvironmentID),
		Name:                       row.Name,
		Provider:                   row.Provider,
		Repo:                       row.Repo,
		CloneURL:                   row.CloneUrl,
		SourceType:                 row.SourceType,
		GitHubAppID:                uuidFromPG(row.GithubAppID),
		DockerfileContent:          row.DockerfileContent,
		BuildArgs:                  unmarshalBuildArgs(row.BuildArgs),
		Branch:                     row.Branch,
		BuildPack:                  row.BuildPack,
		ImageRef:                   row.ImageRef,
		RegistryUsername:           row.RegistryUsername,
		RegistryPasswordCiphertext: row.RegistryPasswordCiphertext,
		ComposeContent:             row.ComposeContent,
		ComposeFile:                row.ComposeFile,
		ComposeService:             row.ComposeService,
		BaseDomain:                 row.BaseDomain,
		BaseDomainDisabled:         row.BaseDomainDisabled,
		IsPreview:                  row.IsPreview,
		Port:                       row.Port,
		HostPort:                   row.HostPort,
		CreatedAt:                  timeFromPG(row.CreatedAt),
		UpdatedAt:                  timeFromPG(row.UpdatedAt),
	}
}

// deployKeyFromRow maps a sqlc deploy-key row to the domain model.
func deployKeyFromRow(row sqlc.ApplicationDeployKey) DeployKey {
	return DeployKey{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		PrivateKeyID:  uuidFromPG(row.PrivateKeyID),
		Provider:      row.Provider,
		Repo:          row.Repo,
		ProviderKeyID: row.ProviderKeyID,
		Fingerprint:   row.Fingerprint,
		PublicKey:     row.PublicKey,
		CreatedAt:     timeFromPG(row.CreatedAt),
	}
}

// envVarParams maps domain env vars to insert rows. The application_id is
// stamped by the transactional store method, so it is left unset here.
func envVarParams(envVars []EnvVar) []sqlc.InsertEnvVarParams {
	params := make([]sqlc.InsertEnvVarParams, 0, len(envVars))
	for _, v := range envVars {
		params = append(params, sqlc.InsertEnvVarParams{Key: v.Key, Value: v.Value})
	}
	return params
}

// secretParams maps sealed secrets to insert rows. A secret keeps the ID the
// service assigned (the stable `secret:<id>` reference the API hands out); an
// unset one is generated here so the row never carries a NULL primary key.
func secretParams(secrets []Secret) []sqlc.InsertSecretParams {
	params := make([]sqlc.InsertSecretParams, 0, len(secrets))
	for _, s := range secrets {
		id := s.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		params = append(params, sqlc.InsertSecretParams{
			ID:         pgUUID(id),
			Key:        s.Key,
			Ciphertext: s.Ciphertext,
		})
	}
	return params
}

// storageParams maps domain storages to insert rows (see envVarParams).
func storageParams(storages []Storage) []sqlc.InsertStorageParams {
	params := make([]sqlc.InsertStorageParams, 0, len(storages))
	for _, s := range storages {
		params = append(params, sqlc.InsertStorageParams{
			Name:          s.Name,
			HostPath:      s.HostPath,
			ContainerPath: s.ContainerPath,
		})
	}
	return params
}

// applicationWriteError classifies a failed application write: the unique
// index on (environment_id, name) surfaces as ErrValidation with a message the API
// can show, a duplicate domain binding as ErrConflict (another application on
// the same node owns the hostname), a foreign key violation on the
// environment (deleted between validation and write) as ErrNotFound, and
// every other failure is wrapped for the log.
func applicationWriteError(err error, name string) error {
	if pgErr := uniqueViolation(err); pgErr != nil {
		if pgErr.ConstraintName == "applications_server_domain_idx" {
			return ErrDomainConflict
		}
		return fmt.Errorf("%w: an application named %q already exists", ErrValidation, name)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrNotFound
	}
	return fmt.Errorf("deploy: write application: %w", err)
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
		// The rendered compose document of a compose run, empty for every
		// other deployment. It may hold substituted secret values and is
		// never returned by the API.
		ComposeDocument: row.ComposeDocument,
		ComposeCommit:   row.ComposeCommit,
		// The git commit the deployment cloned, empty for sources that
		// never clone. The author is the name without the email.
		CommitSHA:     row.CommitSha,
		CommitMessage: row.CommitMessage,
		CommitAuthor:  row.CommitAuthor,
		CommittedAt:   row.CommittedAt,
	}
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), which the active-deployment index raises.
func isUniqueViolation(err error) bool {
	return uniqueViolation(err) != nil
}

// uniqueViolation returns the PostgreSQL unique-constraint violation, if any,
// so callers can distinguish which index fired.
func uniqueViolation(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr
	}
	return nil
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
