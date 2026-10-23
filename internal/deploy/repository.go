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

type Repository interface {
	GetBoundedBuildLog(context.Context, uuid.UUID, uuid.UUID, LogOptions) (string, State, bool, error)
	GetApplication(ctx context.Context, appID uuid.UUID) (Application, error)
	ListApplications(ctx context.Context, scope teams.Scope) ([]Application, error)
	ListApplicationsByEnvironment(ctx context.Context, environmentID uuid.UUID, includePreviews bool) ([]Application, error)
	ListApplicationsByProject(ctx context.Context, projectID uuid.UUID, includePreviews bool) ([]Application, error)
	NameInEnvironment(ctx context.Context, environmentID uuid.UUID, name string, exceptID uuid.UUID) (bool, error)
	ResolveEnvironment(ctx context.Context, environmentID, teamID uuid.UUID) (EnvironmentRef, error)
	ResolveProject(ctx context.Context, projectID, teamID uuid.UUID) (uuid.UUID, error)
	HasLivePreviews(ctx context.Context, appID uuid.UUID) (bool, error)
	CreateApplication(ctx context.Context, app Application, envVars []EnvVar, secrets []Secret, storages []Storage) (Application, error)
	UpdateApplication(ctx context.Context, app Application) (Application, error)
	DeleteApplication(ctx context.Context, appID uuid.UUID) error
	ReplaceEnvVars(ctx context.Context, appID uuid.UUID, envVars []EnvVar, secrets []Secret) error
	ReplaceStorages(ctx context.Context, appID uuid.UUID, storages []Storage) error
	ServerExists(ctx context.Context, serverID uuid.UUID, scope teams.Scope) (bool, error)
	ServerTeam(ctx context.Context, serverID uuid.UUID) (uuid.UUID, bool, error)
	GitHubAppOwnedBy(ctx context.Context, id, userID uuid.UUID) (bool, error)
	CreateDeployment(ctx context.Context, dep Deployment) (Deployment, error)
	GetDeployment(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error)
	ListDeployments(ctx context.Context, appID uuid.UUID) ([]Deployment, error)
	ListDeploymentsLimit(ctx context.Context, appID uuid.UUID, limit int) ([]Deployment, error)
	ListRecentDeployments(ctx context.Context, scope teams.Scope, limit int) ([]ApplicationDeployment, error)
	FailStaleDeployments(ctx context.Context) (int64, error)
	UpdateDeployment(ctx context.Context, dep Deployment) (Deployment, error)
	UpdateDeploymentBuildLog(ctx context.Context, id uuid.UUID, log string) error
	ListEnvVars(ctx context.Context, appID uuid.UUID) ([]EnvVar, error)
	ListSecrets(ctx context.Context, appID uuid.UUID) ([]Secret, error)
	ListEnvConfig(ctx context.Context, appID uuid.UUID) ([]EnvVar, []Secret, error)
	ListStorages(ctx context.Context, appID uuid.UUID) ([]Storage, error)
	GetDeployKey(ctx context.Context, appID uuid.UUID) (DeployKey, error)
	CreateDeployKey(ctx context.Context, key DeployKey, privateKeyPEM string) (DeployKey, error)
	DeleteDeployKey(ctx context.Context, key DeployKey) (DeployKey, error)
	DeletePrivateKey(ctx context.Context, privateKeyID uuid.UUID) error
	DeployKeyPrivatePEM(ctx context.Context, appID uuid.UUID) (string, error)
	UpsertGitCredential(ctx context.Context, appID uuid.UUID, username, sealedToken string) error
	GetGitCredential(ctx context.Context, appID uuid.UUID) (GitCredential, error)
	GitCredential(ctx context.Context, appID uuid.UUID) (username, token string, err error)
	DeleteGitCredential(ctx context.Context, appID uuid.UUID) error
	CopyGitCredential(ctx context.Context, baseID, previewID uuid.UUID) error
	GetCertificateIntent(ctx context.Context, appID uuid.UUID) (CertificateIntent, error)
	GetDNSProviderInfo(ctx context.Context, providerID uuid.UUID) (DNSProviderInfo, error)
	ListSharedVariables(ctx context.Context, projectID, environmentID uuid.UUID) (project []SharedVariable, environment []SharedVariable, err error)
	CreateCertificateIntent(ctx context.Context, in CertificateIntent) error
	CreateCertificateIntentIfAbsent(ctx context.Context, in CertificateIntent) error
	ListCertificateIntents(ctx context.Context, appID uuid.UUID) ([]CertificateIntent, error)
	DeleteAutoCertificateIntent(ctx context.Context, appID uuid.UUID, domain string) error
	ListApplicationDomains(ctx context.Context, appID uuid.UUID) ([]ApplicationDomain, error)
	CreateApplicationDomain(ctx context.Context, domain ApplicationDomain) (ApplicationDomain, error)
	UpdateApplicationDomain(ctx context.Context, domain ApplicationDomain) (ApplicationDomain, error)
	ClearPrimaryApplicationDomains(ctx context.Context, appID uuid.UUID) error
	InTx(ctx context.Context, fn func(tx Repository) error) error
	DeleteApplicationDomain(ctx context.Context, appID, domainID uuid.UUID) error
	DeleteApplicationDomains(ctx context.Context, appID uuid.UUID) error
	DomainClaim(ctx context.Context, domain string) (ApplicationDomain, error)
	ListRedirectSources(ctx context.Context) ([]string, error)
}

type storeRepository struct {
	store  *store.Store
	secret string
}

func newStoreRepository(st *store.Store, secret string) *storeRepository {
	return &storeRepository{store: st, secret: secret}
}

func (r *storeRepository) InTx(ctx context.Context, fn func(tx Repository) error) error {
	return r.store.InTx(ctx, func(tx *store.Store) error {
		return fn(&storeRepository{store: tx, secret: r.secret})
	})
}

func (r *storeRepository) ClearPrimaryApplicationDomains(ctx context.Context, appID uuid.UUID) error {
	if err := r.store.ClearPrimaryApplicationDomains(ctx, pgUUID(appID)); err != nil {
		return fmt.Errorf("deploy: clear primary application domains: %w", err)
	}
	return nil
}

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

func (r *storeRepository) HasLivePreviews(ctx context.Context, appID uuid.UUID) (bool, error) {
	live, err := r.store.HasLivePreviewBindings(ctx, pgUUID(appID))
	if err != nil {
		return false, fmt.Errorf("deploy: live preview check: %w", err)
	}
	return live, nil
}

func (r *storeRepository) enrich(ctx context.Context, rows []sqlc.Application) ([]Application, error) {
	applications := make([]Application, 0, len(rows))
	envs := make(map[uuid.UUID]sqlc.Environment)
	projects := make(map[uuid.UUID]sqlc.Project)
	servers := make(map[uuid.UUID]string)
	primaries := make(map[uuid.UUID]ApplicationDomain)
	if len(rows) > 0 {
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		domainRows, err := r.store.ListPrimaryApplicationDomains(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("deploy: list primary domains: %w", err)
		}
		for _, row := range domainRows {
			primaries[uuidFromPG(row.ApplicationID)] = ApplicationDomain{
				ApplicationID: uuidFromPG(row.ApplicationID),
				Domain:        row.Domain,
				Port:          row.Port,
				Service:       derefString(row.Service),
				Protocol:      row.Protocol,
				IsPrimary:     true,
			}
		}
	}
	for _, row := range rows {
		app := applicationFromRow(row)
		if primary, ok := primaries[app.ID]; ok {
			app.PrimaryDomain = &primary
		}
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

func (r *storeRepository) CreateApplication(
	ctx context.Context,
	app Application,
	envVars []EnvVar,
	secrets []Secret,
	storages []Storage,
) (Application, error) {
	appParams := sqlc.CreateApplicationParams{
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
		SourceType:                 NormalizeSourceType(app.SourceType, app.Provider),
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
	}
	row, err := r.store.CreateApplicationWithConfig(ctx,
		appParams,
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

func (r *storeRepository) UpdateApplication(ctx context.Context, app Application) (Application, error) {
	row, err := r.store.UpdateApplication(ctx, sqlc.UpdateApplicationParams{
		ID:                         pgUUID(app.ID),
		Name:                       app.Name,
		Branch:                     app.Branch,
		BuildPack:                  app.BuildPack,
		ServerID:                   pgUUID(app.ServerID),
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

func (r *storeRepository) DeleteApplication(ctx context.Context, appID uuid.UUID) error {
	if err := r.store.DeleteApplication(ctx, pgUUID(appID)); err != nil {
		return fmt.Errorf("deploy: delete application: %w", err)
	}
	return nil
}

func (r *storeRepository) ReplaceEnvVars(ctx context.Context, appID uuid.UUID, envVars []EnvVar, secrets []Secret) error {
	if err := r.store.ReplaceApplicationEnv(ctx, pgUUID(appID), envVarParams(envVars), secretParams(secrets)); err != nil {
		return fmt.Errorf("deploy: replace env vars: %w", err)
	}
	return nil
}

func (r *storeRepository) ReplaceStorages(ctx context.Context, appID uuid.UUID, storages []Storage) error {
	if err := r.store.ReplaceApplicationStorages(ctx, pgUUID(appID), storageParams(storages)); err != nil {
		return fmt.Errorf("deploy: replace storages: %w", err)
	}
	return nil
}

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

func (r *storeRepository) CreateDeployment(ctx context.Context, dep Deployment) (Deployment, error) {
	row, err := r.store.CreateDeployment(ctx, sqlc.CreateDeploymentParams{
		ApplicationID:   pgUUID(dep.ApplicationID),
		Kind:            string(dep.Kind),
		State:           string(dep.State),
		ImageTag:        dep.ImageTag,
		RegistryImage:   dep.RegistryImage,
		Digest:          dep.Digest,
		RollbackFrom:    pgUUID(dep.RollbackFrom),
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

func (r *storeRepository) ListRecentDeployments(ctx context.Context, scope teams.Scope, limit int) ([]ApplicationDeployment, error) {
	if limit <= 0 {
		limit = 1
	}
	if limit > maxDeploymentListLimit {
		limit = maxDeploymentListLimit
	}
	if scope.Active() {
		rows, err := r.store.ListRecentDeploymentsByTeam(ctx, pgUUID(scope.TeamID), int32(limit))
		if err != nil {
			return nil, fmt.Errorf("deploy: list recent deployments: %w", err)
		}
		out := make([]ApplicationDeployment, 0, len(rows))
		for _, row := range rows {
			out = append(out, applicationDeploymentFromTeamRow(row))
		}
		return out, nil
	}
	rows, err := r.store.ListRecentDeploymentsByUser(ctx, pgUUID(scope.UserID), int32(limit))
	if err != nil {
		return nil, fmt.Errorf("deploy: list recent deployments: %w", err)
	}
	out := make([]ApplicationDeployment, 0, len(rows))
	for _, row := range rows {
		out = append(out, applicationDeploymentFromUserRow(row))
	}
	return out, nil
}

func (r *storeRepository) FailStaleDeployments(ctx context.Context) (int64, error) {
	n, err := r.store.FailStaleDeployments(ctx)
	if err != nil {
		return 0, fmt.Errorf("deploy: fail stale deployments: %w", err)
	}
	return n, nil
}

func (r *storeRepository) UpdateDeployment(ctx context.Context, dep Deployment) (Deployment, error) {
	row, err := r.store.UpdateDeployment(ctx, sqlc.UpdateDeploymentParams{
		ID:              pgUUID(dep.ID),
		State:           string(dep.State),
		ImageTag:        dep.ImageTag,
		RegistryImage:   dep.RegistryImage,
		Digest:          dep.Digest,
		Error:           dep.Error,
		Attempt:         dep.Attempt,
		ContainerID:     dep.ContainerID,
		StartedAt:       pgTime(dep.StartedAt),
		FinishedAt:      pgTime(dep.FinishedAt),
		ComposeDocument: dep.ComposeDocument,
		ComposeCommit:   dep.ComposeCommit,
		CommitSha:       dep.CommitSHA,
		CommitMessage:   dep.CommitMessage,
		CommitAuthor:    dep.CommitAuthor,
		CommittedAt:     dep.CommittedAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, fmt.Errorf("deploy: update deployment: %w", err)
	}
	return deploymentFromRow(row), nil
}

func (r *storeRepository) UpdateDeploymentBuildLog(ctx context.Context, id uuid.UUID, log string) error {
	if err := r.store.UpdateDeploymentBuildLog(ctx, sqlc.UpdateDeploymentBuildLogParams{
		ID:       pgUUID(id),
		BuildLog: log,
	}); err != nil {
		return fmt.Errorf("deploy: update deployment build log: %w", err)
	}
	return nil
}

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

func (r *storeRepository) DeletePrivateKey(ctx context.Context, privateKeyID uuid.UUID) error {
	if err := r.store.DeletePrivateKey(ctx, pgUUID(privateKeyID)); err != nil {
		return fmt.Errorf("deploy: delete private key: %w", err)
	}
	return nil
}

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

func (r *storeRepository) UpsertGitCredential(ctx context.Context, appID uuid.UUID, username, sealedToken string) error {
	if _, err := r.store.UpsertApplicationGitCredential(ctx, pgUUID(appID), username, sealedToken); err != nil {
		return fmt.Errorf("deploy: upsert git credential: %w", err)
	}
	return nil
}

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

func (r *storeRepository) GetCertificateIntent(ctx context.Context, appID uuid.UUID) (CertificateIntent, error) {
	rows, err := r.store.ListDomainCertificatesByApplication(ctx, pgUUID(appID))
	if err != nil {
		return CertificateIntent{}, fmt.Errorf("deploy: get certificate intent: %w", err)
	}
	if len(rows) == 0 {
		return CertificateIntent{}, ErrNotFound
	}
	primary := ""
	if rows, err := r.store.ListApplicationDomainsByApplication(ctx, pgUUID(appID)); err == nil {
		for _, d := range rows {
			if d.IsPrimary {
				primary = d.Domain
				break
			}
		}
		if primary == "" && len(rows) > 0 {
			primary = rows[0].Domain
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return CertificateIntent{}, fmt.Errorf("deploy: get certificate intent: %w", err)
	}
	for _, row := range rows {
		if primary == "" || row.Domain == primary {
			return CertificateIntent{
				ApplicationID: uuidFromPG(row.ApplicationID),
				Domain:        row.Domain,
				Enabled:       row.Enabled,
				Challenge:     row.Challenge,
				DNSProviderID: uuidFromPG(row.DnsProviderID),
				Wildcard:      row.Wildcard,
			}, nil
		}
	}
	return CertificateIntent{}, ErrNotFound
}

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

func (r *storeRepository) CreateCertificateIntentIfAbsent(ctx context.Context, in CertificateIntent) error {
	if _, err := r.store.CreateDomainCertificateIfAbsent(ctx, sqlc.CreateDomainCertificateIfAbsentParams{
		ApplicationID: pgUUID(in.ApplicationID),
		Domain:        in.Domain,
		Enabled:       in.Enabled,
		Challenge:     in.Challenge,
		DnsProviderID: pgUUID(in.DNSProviderID),
		Wildcard:      in.Wildcard,
	}); err != nil {
		return fmt.Errorf("deploy: create certificate intent: %w", err)
	}
	return nil
}

func (r *storeRepository) ListCertificateIntents(ctx context.Context, appID uuid.UUID) ([]CertificateIntent, error) {
	rows, err := r.store.ListDomainCertificatesByApplication(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("deploy: list certificate intents: %w", err)
	}
	intents := make([]CertificateIntent, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, CertificateIntent{
			ApplicationID: uuidFromPG(row.ApplicationID),
			Domain:        row.Domain,
			Enabled:       row.Enabled,
			Challenge:     row.Challenge,
			DNSProviderID: uuidFromPG(row.DnsProviderID),
			Wildcard:      row.Wildcard,
		})
	}
	return intents, nil
}

func (r *storeRepository) DeleteAutoCertificateIntent(ctx context.Context, appID uuid.UUID, domain string) error {
	if _, err := r.store.DeleteAutoDomainCertificate(ctx, sqlc.DeleteAutoDomainCertificateParams{
		ApplicationID: pgUUID(appID),
		Domain:        domain,
	}); err != nil {
		return fmt.Errorf("deploy: delete certificate intent: %w", err)
	}
	return nil
}

func (r *storeRepository) ListApplicationDomains(ctx context.Context, appID uuid.UUID) ([]ApplicationDomain, error) {
	rows, err := r.store.ListApplicationDomainsByApplication(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("deploy: list application domains: %w", err)
	}
	domains := make([]ApplicationDomain, 0, len(rows))
	for _, row := range rows {
		domains = append(domains, applicationDomainFromRow(row))
	}
	return domains, nil
}

func (r *storeRepository) CreateApplicationDomain(ctx context.Context, domain ApplicationDomain) (ApplicationDomain, error) {
	row, err := r.store.CreateApplicationDomain(ctx, sqlc.CreateApplicationDomainParams{
		ApplicationID: pgUUID(domain.ApplicationID),
		Domain:        domain.Domain,
		IsPrimary:     domain.IsPrimary,
		Disabled:      domain.Disabled,
		Port:          domain.Port,
		Service:       nullableString(domain.Service),
		Protocol:      NormalizeProtocol(domain.Protocol),
	})
	if err != nil {
		if pgErr := uniqueViolation(err); pgErr != nil {
			switch pgErr.ConstraintName {
			case "application_domains_global_domain_idx":
				return ApplicationDomain{}, ErrDomainConflict
			case "application_domains_app_domain_idx":
				return ApplicationDomain{}, fmt.Errorf("%w: the domain is already attached to this application", ErrValidation)
			case "application_domains_app_primary_idx":
				return ApplicationDomain{}, fmt.Errorf("%w: the application already has a primary domain", ErrValidation)
			}
		}
		return ApplicationDomain{}, fmt.Errorf("deploy: create application domain: %w", err)
	}
	return applicationDomainFromRow(row), nil
}

func (r *storeRepository) UpdateApplicationDomain(ctx context.Context, domain ApplicationDomain) (ApplicationDomain, error) {
	row, err := r.store.UpdateApplicationDomain(ctx, sqlc.UpdateApplicationDomainParams{
		ID:        pgUUID(domain.ID),
		Domain:    domain.Domain,
		IsPrimary: domain.IsPrimary,
		Disabled:  domain.Disabled,
		Port:      domain.Port,
		Service:   nullableString(domain.Service),
		Protocol:  NormalizeProtocol(domain.Protocol),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ApplicationDomain{}, ErrNotFound
		}
		if pgErr := uniqueViolation(err); pgErr != nil {
			switch pgErr.ConstraintName {
			case "application_domains_global_domain_idx":
				return ApplicationDomain{}, ErrDomainConflict
			case "application_domains_app_domain_idx":
				return ApplicationDomain{}, fmt.Errorf("%w: the domain is already attached to this application", ErrValidation)
			case "application_domains_app_primary_idx":
				return ApplicationDomain{}, fmt.Errorf("%w: the application already has a primary domain", ErrValidation)
			}
		}
		return ApplicationDomain{}, fmt.Errorf("deploy: update application domain: %w", err)
	}
	return applicationDomainFromRow(row), nil
}

func (r *storeRepository) DeleteApplicationDomain(ctx context.Context, appID, domainID uuid.UUID) error {
	if err := r.store.DeleteApplicationDomain(ctx, pgUUID(domainID), pgUUID(appID)); err != nil {
		return fmt.Errorf("deploy: delete application domain: %w", err)
	}
	return nil
}

func (r *storeRepository) DeleteApplicationDomains(ctx context.Context, appID uuid.UUID) error {
	if err := r.store.DeleteApplicationDomainsByApplication(ctx, pgUUID(appID)); err != nil {
		return fmt.Errorf("deploy: delete application domains: %w", err)
	}
	return nil
}

func (r *storeRepository) DomainClaim(ctx context.Context, domain string) (ApplicationDomain, error) {
	row, err := r.store.GetApplicationDomainByNameAnyApp(ctx, domain)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ApplicationDomain{}, ErrNotFound
		}
		return ApplicationDomain{}, fmt.Errorf("deploy: resolve domain claim: %w", err)
	}
	return applicationDomainFromRow(row), nil
}

func (r *storeRepository) ListRedirectSources(ctx context.Context) ([]string, error) {
	rows, err := r.store.ListEnabledRedirects(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy: list redirect sources: %w", err)
	}
	sources := make([]string, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, row.SourceDomain)
	}
	return sources, nil
}

func applicationDomainFromRow(row sqlc.ApplicationDomain) ApplicationDomain {
	domain := ApplicationDomain{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		Domain:        row.Domain,
		IsPrimary:     row.IsPrimary,
		Disabled:      row.Disabled,
		Port:          row.Port,
		Service:       derefString(row.Service),
		Protocol:      NormalizeProtocol(row.Protocol),
	}
	if row.CreatedAt.Valid {
		domain.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		domain.UpdatedAt = row.UpdatedAt.Time
	}
	return domain
}

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
		IsPreview:                  row.IsPreview,
		CreatedAt:                  timeFromPG(row.CreatedAt),
		UpdatedAt:                  timeFromPG(row.UpdatedAt),
	}
}

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

func envVarParams(envVars []EnvVar) []sqlc.InsertEnvVarParams {
	params := make([]sqlc.InsertEnvVarParams, 0, len(envVars))
	for _, v := range envVars {
		params = append(params, sqlc.InsertEnvVarParams{Key: v.Key, Value: v.Value})
	}
	return params
}

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

func applicationWriteError(err error, name string) error {
	if pgErr := uniqueViolation(err); pgErr != nil {
		switch pgErr.ConstraintName {
		case "applications_server_domain_idx",
			"application_domains_global_domain_idx",
			"application_domains_app_domain_idx":
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

func deploymentFromRow(row sqlc.Deployment) Deployment {
	return Deployment{
		ID:              uuidFromPG(row.ID),
		ApplicationID:   uuidFromPG(row.ApplicationID),
		Kind:            Kind(row.Kind),
		State:           State(row.State),
		ImageTag:        row.ImageTag,
		RegistryImage:   row.RegistryImage,
		Digest:          row.Digest,
		Error:           row.Error,
		Attempt:         row.Attempt,
		ContainerID:     row.ContainerID,
		RollbackFrom:    uuidFromPG(row.RollbackFrom),
		StartedAt:       timeFromPG(row.StartedAt),
		FinishedAt:      timeFromPG(row.FinishedAt),
		CreatedAt:       timeFromPG(row.CreatedAt),
		UpdatedAt:       timeFromPG(row.UpdatedAt),
		ComposeDocument: row.ComposeDocument,
		ComposeCommit:   row.ComposeCommit,
		CommitSHA:       row.CommitSha,
		CommitMessage:   row.CommitMessage,
		CommitAuthor:    row.CommitAuthor,
		CommittedAt:     row.CommittedAt,
		BuildLog:        row.BuildLog,
	}
}

func applicationDeploymentFromTeamRow(row sqlc.ListRecentDeploymentsByTeamRow) ApplicationDeployment {
	return ApplicationDeployment{
		Deployment: deploymentFromRow(sqlc.Deployment{
			ID:            row.ID,
			ApplicationID: row.ApplicationID,
			Kind:          row.Kind,
			State:         row.State,
			ImageTag:      row.ImageTag,
			RegistryImage: row.RegistryImage,
			Digest:        row.Digest,
			Error:         row.Error,
			Attempt:       row.Attempt,
			ContainerID:   row.ContainerID,
			RollbackFrom:  row.RollbackFrom,
			StartedAt:     row.StartedAt,
			FinishedAt:    row.FinishedAt,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
			ComposeCommit: row.ComposeCommit,
			CommitSha:     row.CommitSha,
			CommitMessage: row.CommitMessage,
			CommitAuthor:  row.CommitAuthor,
			CommittedAt:   row.CommittedAt,
		}),
		ApplicationName:   row.ApplicationName,
		ApplicationRepo:   row.ApplicationRepo,
		ApplicationBranch: row.ApplicationBranch,
	}
}

func applicationDeploymentFromUserRow(row sqlc.ListRecentDeploymentsByUserRow) ApplicationDeployment {
	return ApplicationDeployment{
		Deployment: deploymentFromRow(sqlc.Deployment{
			ID:            row.ID,
			ApplicationID: row.ApplicationID,
			Kind:          row.Kind,
			State:         row.State,
			ImageTag:      row.ImageTag,
			RegistryImage: row.RegistryImage,
			Digest:        row.Digest,
			Error:         row.Error,
			Attempt:       row.Attempt,
			ContainerID:   row.ContainerID,
			RollbackFrom:  row.RollbackFrom,
			StartedAt:     row.StartedAt,
			FinishedAt:    row.FinishedAt,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
			ComposeCommit: row.ComposeCommit,
			CommitSha:     row.CommitSha,
			CommitMessage: row.CommitMessage,
			CommitAuthor:  row.CommitAuthor,
			CommittedAt:   row.CommittedAt,
		}),
		ApplicationName:   row.ApplicationName,
		ApplicationRepo:   row.ApplicationRepo,
		ApplicationBranch: row.ApplicationBranch,
	}
}

func isUniqueViolation(err error) bool {
	return uniqueViolation(err) != nil
}

func uniqueViolation(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr
	}
	return nil
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func pgTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}
