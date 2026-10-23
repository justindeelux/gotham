package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
)

const secretRefPrefix = "secret:"

type EnvEntry struct {
	Key   string
	Value string
}

type CreateApplicationInput struct {
	Name              string
	EnvironmentID     uuid.UUID
	Provider          string
	Repo              string
	CloneURL          string
	SourceType        string
	GitHubAppID       uuid.UUID
	DockerfileContent string
	BuildArgs         map[string]string
	Branch            string
	BuildPack         string
	ImageRef          string
	RegistryUsername  string
	RegistryPassword  string
	ComposeContent    string
	ComposeFile       string
	ComposeService    string
	ServerID          uuid.UUID
	Env               []EnvEntry
	Storage           []Storage
}

type UpdateApplicationInput struct {
	Name              *string
	Branch            *string
	BuildPack         *string
	ImageRef          *string
	RegistryUsername  *string
	RegistryPassword  *string
	ServerID          *uuid.UUID
	EnvironmentID     *uuid.UUID
	GitHubAppID       *string
	DockerfileContent *string
	BuildArgs         *map[string]string
	ComposeContent    *string
	ComposeFile       *string
	ComposeService    *string
}

type ApplicationFilter struct {
	EnvironmentID   uuid.UUID
	ProjectID       uuid.UUID
	IncludePreviews bool
}

func (in UpdateApplicationInput) empty() bool {
	return in.Name == nil && in.Branch == nil && in.BuildPack == nil &&
		in.ImageRef == nil && in.RegistryUsername == nil && in.RegistryPassword == nil &&
		in.ServerID == nil && in.EnvironmentID == nil && in.GitHubAppID == nil &&
		in.DockerfileContent == nil && in.BuildArgs == nil &&
		in.ComposeContent == nil && in.ComposeFile == nil && in.ComposeService == nil
}

func (s *Service) CreateApplication(ctx context.Context, userID uuid.UUID, in CreateApplicationInput) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	teamID := teamIDFor(ctx, userID)
	if in.EnvironmentID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: environment is required", ErrValidation)
	}
	if _, err := s.repo.ResolveEnvironment(ctx, in.EnvironmentID, teamID); err != nil {
		return Application{}, err
	}
	app := Application{
		ID:                uuid.Nil,
		UserID:            userID,
		TeamID:            teamID,
		EnvironmentID:     in.EnvironmentID,
		Name:              strings.TrimSpace(in.Name),
		Provider:          strings.TrimSpace(in.Provider),
		Repo:              strings.TrimSpace(in.Repo),
		CloneURL:          strings.TrimSpace(in.CloneURL),
		SourceType:        NormalizeSourceType(strings.TrimSpace(in.SourceType), strings.TrimSpace(in.Provider)),
		Branch:            strings.TrimSpace(in.Branch),
		BuildPack:         strings.TrimSpace(in.BuildPack),
		ImageRef:          strings.TrimSpace(in.ImageRef),
		RegistryUsername:  strings.TrimSpace(in.RegistryUsername),
		ComposeContent:    in.ComposeContent,
		ComposeFile:       strings.TrimSpace(in.ComposeFile),
		ComposeService:    strings.TrimSpace(in.ComposeService),
		ServerID:          in.ServerID,
		GitHubAppID:       in.GitHubAppID,
		DockerfileContent: in.DockerfileContent,
		BuildArgs:         normalizeBuildArgs(in.BuildArgs),
	}
	registryPassword := strings.TrimSpace(in.RegistryPassword)
	if err := validateRegistryCredential(app.RegistryUsername, registryPassword); err != nil {
		return Application{}, err
	}
	if app.SourceType == SourceDockerfile {
		app.Repo = ""
		app.CloneURL = ""
		if err := ValidateBuildArgs(in.BuildArgs); err != nil {
			return Application{}, err
		}
	}
	if app.SourceType == SourceCompose {
		app.BuildPack = ""
		if strings.TrimSpace(app.ComposeContent) != "" {
			app.Repo = ""
			app.CloneURL = ""
			app.ComposeFile = ""
		}
	}
	if app.Branch == "" && !BranchDefaultsToRemote(app.SourceType, app.Provider) &&
		NormalizeSourceType(app.SourceType, app.Provider) != SourceImage {
		app.Branch = defaultBranch
	}
	if app.SourceType == SourceCompose && strings.TrimSpace(app.ComposeContent) != "" {
		app.Branch = ""
	}
	if err := validateApplication(app, true); err != nil {
		return Application{}, err
	}
	if requestreplay.From(ctx) != nil {
		if !teams.ScopeFor(ctx, userID).CanWrite() {
			return Application{}, teams.ErrForbidden
		}
		if err := fingerprintApplication(ctx, s.secret, app, registryPassword, in); err != nil {
			return Application{}, err
		}
		if err := s.lookupReplay(ctx); err != nil {
			return Application{}, err
		}
		if result := requestreplay.ResultFrom(ctx); result != nil {
			existing, err := s.application(ctx, userID, result.ResourceID, true)
			if err != nil {
				return Application{}, err
			}
			if existing.EnvironmentID != in.EnvironmentID {
				return Application{}, requestreplay.ErrMissing
			}
			return existing, nil
		}
	}
	if registryPassword != "" {
		sealed, err := providers.SealSecret(s.secret, registryPassword)
		if err != nil {
			return Application{}, fmt.Errorf("deploy: seal registry credential: %w", err)
		}
		app.RegistryPasswordCiphertext = sealed
	}
	if app.GitHubAppID != uuid.Nil {
		owned, err := s.repo.GitHubAppOwnedBy(ctx, app.GitHubAppID, userID)
		if err != nil {
			return Application{}, err
		}
		if !owned {
			return Application{}, ErrNotFound
		}
	}
	if err := s.validateServer(ctx, userID, app.ServerID); err != nil {
		return Application{}, err
	}
	envVars, secrets, err := s.prepareEnv(ctx, uuid.Nil, in.Env)
	if err != nil {
		return Application{}, err
	}
	app.ID = uuid.New()
	storages, err := normalizeStorages(app.ID, in.Storage)
	if err != nil {
		return Application{}, err
	}
	created, err := s.repo.CreateApplication(ctx, app, envVars, secrets, storages)
	if err != nil {
		return Application{}, err
	}
	return created, nil
}

func (s *Service) InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) (attempted bool, err error) {
	lifecycle := s.hookLifecycle()
	if lifecycle == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.hookTimeout)
	defer cancel()
	err = lifecycle.InstallHook(ctx, userID, appID, r)
	if err != nil {
		s.logger.Warn("deploy: webhook not installed for the new application; retry with POST /v1/applications/{id}/webhooks",
			"application_id", appID, "error", err)
	}
	return true, err
}

func (s *Service) ListApplications(ctx context.Context, userID uuid.UUID, filter ApplicationFilter) ([]Application, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("deploy: repository is not configured")
	}
	if filter.EnvironmentID != uuid.Nil && filter.ProjectID != uuid.Nil {
		return nil, fmt.Errorf("%w: environment_id and project_id are mutually exclusive", ErrValidation)
	}
	if filter.EnvironmentID != uuid.Nil {
		if _, err := s.repo.ResolveEnvironment(ctx, filter.EnvironmentID, teamIDFor(ctx, userID)); err != nil {
			return nil, err
		}
		return s.repo.ListApplicationsByEnvironment(ctx, filter.EnvironmentID, filter.IncludePreviews)
	}
	if filter.ProjectID != uuid.Nil {
		if _, err := s.repo.ResolveProject(ctx, filter.ProjectID, teamIDFor(ctx, userID)); err != nil {
			return nil, err
		}
		return s.repo.ListApplicationsByProject(ctx, filter.ProjectID, filter.IncludePreviews)
	}
	applications, err := s.repo.ListApplications(ctx, teams.ScopeFor(ctx, userID))
	if err != nil {
		return nil, err
	}
	if applications == nil {
		return []Application{}, nil
	}
	return applications, nil
}

func (s *Service) GetApplication(ctx context.Context, userID, appID uuid.UUID) (Application, error) {
	return s.application(ctx, userID, appID, false)
}

func (s *Service) UpdateApplication(ctx context.Context, userID, appID uuid.UUID, in UpdateApplicationInput) (Application, error) {
	if !Enabled() {
		return Application{}, ErrDisabled
	}
	if in.empty() {
		return Application{}, fmt.Errorf("%w: no fields to update", ErrValidation)
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	if in.ServerID != nil && *in.ServerID != app.ServerID {
		if open, err := s.repo.HasLivePreviews(ctx, app.ID); err != nil {
			return Application{}, err
		} else if open {
			return Application{}, ErrPreviewsOpen
		}
		if err := s.rejectInFlight(ctx, app.ID); err != nil {
			if errors.Is(err, ErrConflict) {
				return Application{}, ErrDeployInFlight
			}
			return Application{}, err
		}
	}
	movedEnvironment := false
	if in.EnvironmentID != nil && *in.EnvironmentID != app.EnvironmentID {
		if open, err := s.repo.HasLivePreviews(ctx, app.ID); err != nil {
			return Application{}, err
		} else if open {
			return Application{}, ErrPreviewsOpen
		}
		if _, err := s.repo.ResolveEnvironment(ctx, *in.EnvironmentID, app.TeamID); err != nil {
			return Application{}, err
		}
		name := app.Name
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		collision, err := s.repo.NameInEnvironment(ctx, *in.EnvironmentID, name, app.ID)
		if err != nil {
			return Application{}, err
		}
		if collision {
			return Application{}, fmt.Errorf("%w: an application named %q already exists in the target environment", ErrNameConflict, name)
		}
		app.EnvironmentID = *in.EnvironmentID
		movedEnvironment = true
	}
	previousServer := app.ServerID
	if in.Name != nil {
		app.Name = strings.TrimSpace(*in.Name)
	}
	isImage := NormalizeSourceType(app.SourceType, app.Provider) == SourceImage
	if in.ImageRef != nil || in.RegistryUsername != nil || in.RegistryPassword != nil {
		if !isImage {
			return Application{}, fmt.Errorf("%w: image fields need source type %q", ErrValidation, SourceImage)
		}
	}
	if isImage {
		if in.Branch != nil && strings.TrimSpace(*in.Branch) != "" {
			return Application{}, fmt.Errorf("%w: source type %q carries no branch", ErrValidation, SourceImage)
		}
		if in.BuildPack != nil && strings.TrimSpace(*in.BuildPack) != "" {
			return Application{}, fmt.Errorf("%w: source type %q carries no build pack", ErrValidation, SourceImage)
		}
	}
	if in.ImageRef != nil {
		ref := strings.TrimSpace(*in.ImageRef)
		if err := ValidateImageReference(ref); err != nil {
			return Application{}, err
		}
		hostChanged := RegistryHost(app.ImageRef) != RegistryHost(ref)
		if hostChanged && app.RegistryPasswordCiphertext != "" && in.RegistryPassword == nil {
			return Application{}, fmt.Errorf("%w: image reference moves to another registry host: re-enter the registry password or clear the credential",
				ErrValidation)
		}
		if hostChanged && in.RegistryUsername == nil {
			app.RegistryUsername = ""
		}
		app.ImageRef = ref
	}
	if in.RegistryUsername != nil {
		app.RegistryUsername = strings.TrimSpace(*in.RegistryUsername)
		if len(app.RegistryUsername) > maxRegistryUsernameLen {
			return Application{}, fmt.Errorf("%w: registry username is too long", ErrValidation)
		}
	}
	if in.RegistryPassword != nil {
		password := strings.TrimSpace(*in.RegistryPassword)
		if len(password) > maxRegistryPasswordLen {
			return Application{}, fmt.Errorf("%w: registry password is too long", ErrValidation)
		}
		if password == "" {
			app.RegistryPasswordCiphertext = ""
		} else {
			sealed, err := providers.SealSecret(s.secret, password)
			if err != nil {
				return Application{}, fmt.Errorf("deploy: seal registry credential: %w", err)
			}
			app.RegistryPasswordCiphertext = sealed
		}
	}
	if err := requireCompleteRegistryCredential(app.RegistryUsername, app.RegistryPasswordCiphertext != ""); err != nil {
		return Application{}, err
	}
	if in.Branch != nil {
		app.Branch = strings.TrimSpace(*in.Branch)
	}
	if in.BuildPack != nil {
		app.BuildPack = strings.TrimSpace(*in.BuildPack)
	}
	if in.DockerfileContent != nil {
		if app.SourceType != SourceDockerfile {
			return Application{}, fmt.Errorf("%w: dockerfile content is only valid for source type %q",
				ErrValidation, SourceDockerfile)
		}
		if err := ValidateDockerfileContent(*in.DockerfileContent); err != nil {
			return Application{}, err
		}
		app.DockerfileContent = *in.DockerfileContent
	}
	if in.BuildArgs != nil {
		if app.SourceType != SourceDockerfile {
			return Application{}, fmt.Errorf("%w: build args are only valid for source type %q",
				ErrValidation, SourceDockerfile)
		}
		if err := ValidateBuildArgs(*in.BuildArgs); err != nil {
			return Application{}, err
		}
		app.BuildArgs = normalizeBuildArgs(*in.BuildArgs)
	}
	if in.ComposeContent != nil || in.ComposeFile != nil || in.ComposeService != nil {
		var err error
		app, err = applyComposeUpdate(app, in)
		if err != nil {
			return Application{}, err
		}
	}
	if in.ServerID != nil {
		if *in.ServerID == uuid.Nil {
			return Application{}, fmt.Errorf("%w: server is required", ErrValidation)
		}
		if err := s.validateServer(ctx, userID, *in.ServerID); err != nil {
			return Application{}, err
		}
		app.ServerID = *in.ServerID
	}
	if in.GitHubAppID != nil {
		raw := strings.TrimSpace(*in.GitHubAppID)
		if raw == "" {
			app.GitHubAppID = uuid.Nil
		} else {
			id, err := uuid.Parse(raw)
			if err != nil {
				return Application{}, fmt.Errorf("%w: github_app_id is not a UUID", ErrValidation)
			}
			owned, err := s.repo.GitHubAppOwnedBy(ctx, id, userID)
			if err != nil {
				return Application{}, err
			}
			if !owned {
				return Application{}, ErrNotFound
			}
			app.GitHubAppID = id
		}
		if app.GitHubAppID != uuid.Nil && app.SourceType != SourceGitHubApp {
			return Application{}, fmt.Errorf("%w: github_app_id needs source type %q",
				ErrValidation, SourceGitHubApp)
		}
		if err := validateLinkedCloneURL(app); err != nil {
			return Application{}, err
		}
	}
	if app.Branch == "" && !BranchDefaultsToRemote(app.SourceType, app.Provider) &&
		NormalizeSourceType(app.SourceType, app.Provider) != SourceImage {
		app.Branch = defaultBranch
	}
	if app.SourceType == SourceCompose && strings.TrimSpace(app.ComposeContent) != "" {
		app.Branch = ""
	}
	if isComposeApp(app) {
		if err := validateComposeTarget(app); err != nil {
			return Application{}, err
		}
	}
	if err := validateApplication(app, false); err != nil {
		return Application{}, err
	}
	updated, updateErr := s.repo.UpdateApplication(ctx, app)
	if updateErr != nil {
		if movedEnvironment && errors.Is(updateErr, ErrValidation) &&
			strings.Contains(updateErr.Error(), "already exists") {
			return Application{}, fmt.Errorf("%w: an application named %q already exists in the target environment",
				ErrNameConflict, app.Name)
		}
		return Application{}, updateErr
	}
	moved := updated.ServerID != previousServer
	if moved {
		s.stopContainerOnNode(ctx, previousServer, s.latestContainer(ctx, app.ID))
		s.composeDownApp(ctx, previousServer, updated)
		s.syncProxyServer(ctx, previousServer)
		if updated.PrimaryDomain != nil {
			s.syncProxyServer(ctx, updated.ServerID)
		}
	}
	return updated, nil
}

func (s *Service) DeleteApplication(ctx context.Context, userID, appID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	if err := s.rejectInFlight(ctx, app.ID); err != nil {
		return err
	}
	if s.previewCleanup != nil {
		if err := s.previewCleanup(ctx, app.ID); err != nil {
			return err
		}
	}
	if !app.IsPreview && supportedSourceProvider(app.Provider) && strings.TrimSpace(app.Repo) != "" {
		if lifecycle := s.hookLifecycle(); lifecycle != nil {
			hookCtx, cancel := context.WithTimeout(ctx, s.hookTimeout)
			err := lifecycle.RemoveHook(hookCtx, userID, app.ID)
			cancel()
			if err != nil {
				return err
			}
		}
	}
	if app.IsPreview {
		if key, err := s.repo.GetDeployKey(ctx, app.ID); err == nil {
			if err := s.deleteLocalDeployKey(ctx, key); err != nil {
				s.logger.Warn("deploy: preview deploy key row could not be detached; the application still deletes",
					"application_id", app.ID, "error", err)
			}
		} else if !errors.Is(err, ErrNotFound) {
			s.logger.Warn("deploy: preview deploy key lookup failed; the application still deletes",
				"application_id", app.ID, "error", err)
		}
	} else if err := s.detachDeployKey(ctx, app); err != nil {
		s.logger.Warn("deploy: deploy key could not be detached; the application still deletes",
			"application_id", app.ID, "provider", app.Provider, "repo", app.Repo, "error", err)
	}
	s.composeDownApp(ctx, app.ServerID, app)
	s.removeApplicationContainers(ctx, app)
	if err := s.repo.DeleteApplication(ctx, appID); err != nil {
		return err
	}
	if app.PrimaryDomain != nil {
		s.syncProxyServer(ctx, app.ServerID)
	}
	return nil
}

func (s *Service) GetEnv(ctx context.Context, userID, appID uuid.UUID) ([]EnvEntry, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	return s.envEntries(ctx, appID)
}

func (s *Service) ReplaceEnv(ctx context.Context, userID, appID uuid.UUID, entries []EnvEntry) ([]EnvEntry, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	if _, err := s.application(ctx, userID, appID, true); err != nil {
		return nil, err
	}
	envVars, secrets, err := s.prepareEnv(ctx, appID, entries)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceEnvVars(ctx, appID, envVars, secrets); err != nil {
		return nil, err
	}
	return s.envEntries(ctx, appID)
}

func (s *Service) GetStorages(ctx context.Context, userID, appID uuid.UUID) ([]Storage, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	storages, err := s.repo.ListStorages(ctx, appID)
	if err != nil {
		return nil, err
	}
	if storages == nil {
		return []Storage{}, nil
	}
	return storages, nil
}

func (s *Service) ReplaceStorages(ctx context.Context, userID, appID uuid.UUID, storages []Storage) ([]Storage, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return nil, err
	}
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceCompose {
		return nil, fmt.Errorf(
			"%w: compose applications declare volumes in the compose source (mounted under %s/%s/<name>); update services[].volumes instead of storage mappings",
			ErrValidation, managedVolumeRoot(), appID,
		)
	}
	normalized, err := normalizeStorages(appID, storages)
	if err != nil {
		return nil, err
	}
	s.warnStorageChanges(ctx, appID, storages, normalized)
	if err := s.repo.ReplaceStorages(ctx, appID, normalized); err != nil {
		return nil, err
	}
	return s.GetStorages(ctx, userID, appID)
}

func (s *Service) warnStorageChanges(ctx context.Context, appID uuid.UUID, input, normalized []Storage) {
	existing, err := s.repo.ListStorages(ctx, appID)
	if err != nil {
		return
	}
	byName := make(map[string]Storage, len(existing))
	for _, row := range existing {
		byName[row.Name] = row
	}
	for index, row := range normalized {
		previous, ok := byName[row.Name]
		if !ok || strings.TrimSpace(previous.HostPath) == "" || previous.HostPath == row.HostPath {
			continue
		}
		if !strings.HasPrefix(previous.HostPath, "/") {
			s.logger.Warn("deploy: storage named volume renamed; the old volume's data is not moved",
				"application_id", appID, "storage", row.Name,
				"previous_volume", previous.HostPath, "new_volume", row.HostPath)
			continue
		}
		if index < len(input) && strings.TrimSpace(input[index].HostPath) == "" {
			s.logger.Warn("deploy: storage host path blanked; existing data is not moved to the managed path",
				"application_id", appID, "storage", row.Name,
				"previous_host_path", previous.HostPath, "new_host_path", row.HostPath)
		}
	}
}

func (s *Service) Stop(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	return s.controlContainer(ctx, userID, appID, false)
}

func (s *Service) Start(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	return s.controlContainer(ctx, userID, appID, true)
}

func (s *Service) controlContainer(ctx context.Context, userID, appID uuid.UUID, start bool) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	target, err := s.controlTarget(ctx, app)
	if err != nil {
		return Deployment{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	node, err := s.dialNode(callCtx, app.ServerID)
	if err != nil {
		return Deployment{}, err
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "deployment_id", target.ID, "error", err)
		}
	}()
	if start {
		err = node.Start(callCtx, target.ContainerID)
	} else {
		err = node.Stop(callCtx, target.ContainerID)
	}
	if err != nil {
		return Deployment{}, err
	}
	return target, nil
}

func (s *Service) controlTarget(ctx context.Context, app Application) (Deployment, error) {
	if app.ServerID == uuid.Nil {
		return Deployment{}, ErrServerNotFound
	}
	deployments, err := s.repo.ListDeployments(ctx, app.ID)
	if err != nil {
		return Deployment{}, err
	}
	for _, dep := range deployments {
		if !dep.State.Terminal() {
			return Deployment{}, fmt.Errorf("%w: a deployment is in progress", ErrConflict)
		}
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			return dep, nil
		}
	}
	return Deployment{}, fmt.Errorf("%w: application has no running container", ErrNotFound)
}

func (s *Service) rejectInFlight(ctx context.Context, appID uuid.UUID) error {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		return err
	}
	for _, dep := range deployments {
		if !dep.State.Terminal() {
			return fmt.Errorf("%w: a deployment is in progress", ErrConflict)
		}
	}
	return nil
}

func (s *Service) latestContainer(ctx context.Context, appID uuid.UUID) string {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		s.logger.Warn("deploy: lookup container failed", "application_id", appID, "error", err)
		return ""
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			return dep.ContainerID
		}
	}
	return ""
}

const containerCleanupTimeout = 15 * time.Second

const composeCleanupTimeout = 2 * time.Minute

func (s *Service) composeDownApp(ctx context.Context, serverID uuid.UUID, app Application) {
	if NormalizeSourceType(app.SourceType, app.Provider) != SourceCompose {
		return
	}
	if serverID == uuid.Nil || s.dial == nil {
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), composeCleanupTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "gotham-compose-down-*")
	if err != nil {
		s.logger.Warn("deploy: best-effort compose teardown skipped", "application_id", app.ID, "error", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	content, _, err := s.resolveComposeContent(opCtx, app, dir, nil)
	if err != nil {
		s.logger.Error("deploy: compose teardown cannot resolve the stored document; tearing down by project name",
			"application_id", app.ID, "error", err)
		content = teardownStubDocument()
	} else if rendered, _, _, renderErr := s.renderComposeDocument(opCtx, app, content); renderErr == nil {
		content = rendered
	} else {
		s.logger.Error("deploy: compose teardown cannot render the stored document; tearing down by project name",
			"application_id", app.ID, "error", renderErr)
		content = teardownStubDocument()
	}
	node, err := s.dialNode(opCtx, serverID)
	if err != nil {
		s.logger.Warn("deploy: best-effort compose teardown skipped", "application_id", app.ID, "error", err)
		return
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "error", err)
		}
	}()
	if err := s.probeComposeConfinement(opCtx, node, app); err != nil {
		s.logger.Warn("deploy: compose teardown proceeds without verified confinement",
			"application_id", app.ID, "error", err)
	}
	if err := node.ComposeDown(opCtx, services.ProjectName(app.ID), []byte(content)); err != nil {
		s.logger.Warn("deploy: best-effort compose teardown failed", "application_id", app.ID, "error", err)
	}
}

func (s *Service) stopContainerOnNode(ctx context.Context, serverID uuid.UUID, containerID string) {
	if serverID == uuid.Nil || containerID == "" || s.dial == nil {
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerCleanupTimeout)
	defer cancel()
	node, err := s.dialNode(opCtx, serverID)
	if err != nil {
		s.logger.Warn("deploy: best-effort stop skipped", "server_id", serverID, "error", err)
		return
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "error", err)
		}
	}()
	if err := node.Stop(opCtx, containerID); err != nil {
		s.logger.Warn("deploy: best-effort stop failed", "container_id", containerID, "error", err)
	}
}

func (s *Service) removeApplicationContainers(ctx context.Context, app Application) {
	if app.ServerID == uuid.Nil || s.dial == nil {
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), containerCleanupTimeout)
	defer cancel()
	targets := make(map[string]bool, 2)
	deployments, err := s.repo.ListDeployments(opCtx, app.ID)
	if err != nil {
		s.logger.Warn("deploy: lookup containers for delete failed", "application_id", app.ID, "error", err)
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			targets[dep.ContainerID] = true
		}
	}

	node, err := s.dialNode(opCtx, app.ServerID)
	if err != nil {
		s.logger.Warn("deploy: best-effort container removal skipped", "application_id", app.ID, "error", err)
		return
	}
	defer func() {
		if err := node.Close(); err != nil {
			s.logger.Debug("deploy: close agent connection", "error", err)
		}
	}()
	containers, listErr := node.Containers(opCtx)
	if listErr != nil {
		s.logger.Warn("deploy: container listing failed during delete; removing recorded containers only",
			"application_id", app.ID, "error", listErr)
	} else {
		for _, candidate := range containers {
			if candidate.GetLabels()[labelAppID] == app.ID.String() {
				targets[candidate.GetId()] = true
			}
		}
	}
	for id := range targets {
		if err := node.Remove(opCtx, id); err != nil {
			s.logger.Warn("deploy: best-effort container removal failed",
				"application_id", app.ID, "container_id", id, "error", err)
		}
	}
}

func teamIDFor(ctx context.Context, userID uuid.UUID) uuid.UUID {
	scope := teams.ScopeFor(ctx, userID)
	if scope.Active() {
		return scope.TeamID
	}
	return teams.PersonalTeamID(userID)
}

func (s *Service) validateServer(ctx context.Context, userID, serverID uuid.UUID) error {
	if serverID == uuid.Nil {
		return fmt.Errorf("%w: server is required", ErrValidation)
	}
	known, err := s.repo.ServerExists(ctx, serverID, teams.ScopeFor(ctx, userID))
	if err != nil {
		return err
	}
	if !known {
		return ErrServerNotFound
	}
	return nil
}

func applyComposeUpdate(app Application, in UpdateApplicationInput) (Application, error) {
	if app.SourceType != SourceCompose {
		return Application{}, fmt.Errorf("%w: compose fields are only valid for source type %q",
			ErrValidation, SourceCompose)
	}
	if in.ComposeContent != nil {
		app.ComposeContent = *in.ComposeContent
		if strings.TrimSpace(app.ComposeContent) != "" {
			app.Repo = ""
			app.CloneURL = ""
			app.Branch = ""
			app.ComposeFile = ""
		}
	}
	if in.ComposeFile != nil {
		app.ComposeFile = strings.TrimSpace(*in.ComposeFile)
	}
	if in.ComposeService != nil {
		app.ComposeService = strings.TrimSpace(*in.ComposeService)
	}
	if err := validateComposeSource(app); err != nil {
		return Application{}, err
	}
	return app, nil
}

func validateApplication(app Application, checkSource bool) error {
	if app.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if !ValidSourceType(app.SourceType) {
		return fmt.Errorf("%w: unknown source type %q", ErrValidation, app.SourceType)
	}
	if checkSource {
		if err := validateSource(app); err != nil {
			return err
		}
	}
	if _, err := builds.ParseEngineKind(app.BuildPack); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

const (
	maxRegistryUsernameLen = 255
	maxRegistryPasswordLen = 4096
)

func validateRegistryCredential(username, password string) error {
	if err := requireCompleteRegistryCredential(username, password != ""); err != nil {
		return err
	}
	if len(username) > maxRegistryUsernameLen {
		return fmt.Errorf("%w: registry username is too long", ErrValidation)
	}
	if len(password) > maxRegistryPasswordLen {
		return fmt.Errorf("%w: registry password is too long", ErrValidation)
	}
	return nil
}

func requireCompleteRegistryCredential(username string, hasPassword bool) error {
	hasUser := username != ""
	if (hasUser && !hasPassword) || (!hasUser && hasPassword) {
		return fmt.Errorf("%w: registry credential needs both a username and a password", ErrValidation)
	}
	return nil
}

func validateSource(app Application) error {
	if app.GitHubAppID != uuid.Nil && app.SourceType != SourceGitHubApp {
		return fmt.Errorf("%w: github_app_id needs source type %q",
			ErrValidation, SourceGitHubApp)
	}
	if err := validateLinkedCloneURL(app); err != nil {
		return err
	}
	if app.SourceType != SourceImage &&
		(app.ImageRef != "" || app.RegistryUsername != "" || app.RegistryPasswordCiphertext != "") {
		return fmt.Errorf("%w: image fields need source type %q", ErrValidation, SourceImage)
	}
	switch app.SourceType {
	case "", SourceGitPublic:
		if app.Provider == "github" || app.Provider == "gitlab" {
			return fmt.Errorf("%w: provider %q owns the %q_app source type: use it instead of %q",
				ErrValidation, app.Provider, app.Provider, SourceGitPublic)
		}
		return ValidatePublicGitURL(app.CloneURL)
	case SourceGitHubApp:
		return validateProviderSource(app, "github")
	case SourceGitLabApp:
		return validateProviderSource(app, "gitlab")
	case SourceGitPrivate:
		if app.Provider != "" {
			return fmt.Errorf("%w: source type %q takes no provider: use the %q_app type for repositories on a connected provider",
				ErrValidation, SourceGitPrivate, app.Provider)
		}
		return ValidatePrivateGitURL(app.CloneURL)
	case SourceDockerfile:
		if strings.TrimSpace(app.Provider) != "" {
			return fmt.Errorf("%w: source type %q requires an empty provider, got %q",
				ErrValidation, app.SourceType, app.Provider)
		}
		if err := ValidateDockerfileContent(app.DockerfileContent); err != nil {
			return err
		}
		return ValidateBuildArgs(app.BuildArgs)
	case SourceImage:
		if app.Provider != "" || app.Repo != "" || app.CloneURL != "" {
			return fmt.Errorf("%w: source type %q carries no repository: provider, repo and clone URL must be empty",
				ErrValidation, SourceImage)
		}
		if app.Branch != "" || app.BuildPack != "" {
			return fmt.Errorf("%w: source type %q carries no branch or build pack",
				ErrValidation, SourceImage)
		}
		if app.DockerfileContent != "" || len(app.BuildArgs) != 0 {
			return fmt.Errorf("%w: source type %q carries no Dockerfile or build args",
				ErrValidation, SourceImage)
		}
		return ValidateImageReference(app.ImageRef)
	case SourceCompose:
		return validateComposeSource(app)
	default:
		return fmt.Errorf("%w: unknown source type %q", ErrValidation, app.SourceType)
	}
}

func validateComposeSource(app Application) error {
	pasted := strings.TrimSpace(app.ComposeContent) != ""
	repo := strings.TrimSpace(app.Repo) != "" || strings.TrimSpace(app.CloneURL) != "" ||
		strings.TrimSpace(app.ComposeFile) != ""
	if pasted == repo {
		return fmt.Errorf("%w: compose source needs either pasted content or a repository with a compose file, not both",
			ErrValidation)
	}
	if pasted {
		if strings.TrimSpace(app.Provider) != "" {
			return fmt.Errorf("%w: source type %q with pasted content requires an empty provider, got %q",
				ErrValidation, app.SourceType, app.Provider)
		}
		if err := ValidateComposeContent(app.ComposeContent, app.ID); err != nil {
			return err
		}
		return ValidateComposeService(app.ComposeContent, app.ComposeService)
	}
	switch app.Provider {
	case "github":
		if err := validateProviderSource(app, "github"); err != nil {
			return err
		}
	case "gitlab":
		if err := validateProviderSource(app, "gitlab"); err != nil {
			return err
		}
	default:
		if err := ValidatePublicGitURL(app.CloneURL); err != nil {
			return err
		}
	}
	if err := ValidateComposeFilePath(app.ComposeFile); err != nil {
		return err
	}
	return ValidateComposeService("", app.ComposeService)
}

func validateLinkedCloneURL(app Application) error {
	if app.GitHubAppID == uuid.Nil {
		return nil
	}
	raw := strings.TrimSpace(app.CloneURL)
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return nil
	}
	if strings.EqualFold(scheme, "http") {
		return fmt.Errorf("%w: a linked github_app application needs an https clone URL", ErrValidation)
	}
	return nil
}

func validateProviderSource(app Application, provider string) error {
	if app.Provider != provider {
		return fmt.Errorf("%w: source type %q requires provider %q, got %q",
			ErrValidation, app.SourceType, provider, app.Provider)
	}
	if strings.TrimSpace(app.Repo) == "" {
		return fmt.Errorf("%w: repository is required", ErrValidation)
	}
	return validateCloneURL(app.CloneURL)
}

func (s *Service) prepareEnv(ctx context.Context, appID uuid.UUID, entries []EnvEntry) ([]EnvVar, []Secret, error) {
	existing, err := s.repo.ListSecrets(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	byReference := make(map[string]Secret, len(existing))
	for _, secret := range existing {
		byReference[secret.ID.String()] = secret
	}

	envVars := make([]EnvVar, 0, len(entries))
	secrets := make([]Secret, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if err := validateEnvKey(key); err != nil {
			return nil, nil, err
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("%w: duplicate environment key %q", ErrValidation, key)
		}
		seen[key] = true

		if !strings.HasPrefix(entry.Value, secretRefPrefix) {
			if err := checkTextBytes(entry.Value, fmt.Sprintf("environment variable %q value", key)); err != nil {
				return nil, nil, err
			}
			envVars = append(envVars, EnvVar{Key: key, Value: entry.Value})
			continue
		}
		reference := strings.TrimSpace(strings.TrimPrefix(entry.Value, secretRefPrefix))
		if reference == "" {
			return nil, nil, fmt.Errorf("%w: secret %q has no value", ErrValidation, key)
		}
		if stored, ok := byReference[reference]; ok && stored.Key == key {
			secrets = append(secrets, Secret{
				ID:         stored.ID,
				Key:        key,
				Ciphertext: stored.Ciphertext,
			})
			continue
		}
		if _, err := uuid.Parse(reference); err == nil {
			return nil, nil, fmt.Errorf("%w: unknown secret reference for %q", ErrValidation, key)
		}
		if err := checkTextBytes(reference, fmt.Sprintf("secret %q value", key)); err != nil {
			return nil, nil, err
		}
		ciphertext, err := providers.SealSecret(s.secret, reference)
		if err != nil {
			return nil, nil, fmt.Errorf("deploy: seal secret %s: %w", key, err)
		}
		secrets = append(secrets, Secret{ID: uuid.New(), Key: key, Ciphertext: ciphertext})
	}
	return envVars, secrets, nil
}

func validateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: environment variable name is required", ErrValidation)
	}
	if len(key) > 128 {
		return fmt.Errorf("%w: environment variable name is too long", ErrValidation)
	}
	if strings.ContainsAny(key, "= \t\r\n\x00") {
		return fmt.Errorf("%w: environment variable name %q must not contain spaces, '=' or NUL", ErrValidation, key)
	}
	return nil
}

func normalizeStorages(appID uuid.UUID, storages []Storage) ([]Storage, error) {
	root := managedVolumeRoot()
	normalized := make([]Storage, 0, len(storages))
	seen := make(map[string]bool, len(storages))
	seenDirs := make(map[string]string, len(storages))
	seenNamed := make(map[string]string, len(storages))
	for _, storage := range storages {
		row := Storage{
			Name:          strings.TrimSpace(storage.Name),
			HostPath:      strings.TrimSpace(storage.HostPath),
			ContainerPath: strings.TrimSpace(storage.ContainerPath),
		}
		if row.Name == "" {
			return nil, fmt.Errorf("%w: storage name is required", ErrValidation)
		}
		if seen[row.Name] {
			return nil, fmt.Errorf("%w: duplicate storage name %q", ErrValidation, row.Name)
		}
		seen[row.Name] = true
		dir := storageDirName(row.Name)
		if other, ok := seenDirs[dir]; ok {
			return nil, fmt.Errorf("%w: storage names %q and %q map to the same managed directory %q",
				ErrValidation, other, row.Name, dir)
		}
		seenDirs[dir] = row.Name
		host, err := managedHostPath(root, appID, row.Name, row.HostPath)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(host, "/") {
			if other, ok := seenNamed[host]; ok {
				return nil, fmt.Errorf("%w: storage names %q and %q map to the same named volume %q",
					ErrValidation, other, row.Name, host)
			}
			seenNamed[host] = row.Name
		}
		row.HostPath = host
		normalized = append(normalized, row)
	}
	if _, err := volumeSpecs(root, appID, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (s *Service) envEntries(ctx context.Context, appID uuid.UUID) ([]EnvEntry, error) {
	envVars, secrets, err := s.repo.ListEnvConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	entries := make([]EnvEntry, 0, len(envVars)+len(secrets))
	for _, variable := range envVars {
		entries = append(entries, EnvEntry{Key: variable.Key, Value: variable.Value})
	}
	for _, secret := range secrets {
		entries = append(entries, EnvEntry{Key: secret.Key, Value: secretRefPrefix + secret.ID.String()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}
