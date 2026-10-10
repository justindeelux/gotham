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
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
)

// secretRefPrefix marks an environment value as a sealed secret. Written by an
// API client the rest of the value is the plaintext to seal; read back from the
// API it is the reference (`secret:<secrets row id>`) of the sealed row. The
// prefix is what the FE EnvEditor renders as its "sealed secret" badge, and the
// plaintext never reaches a response body.
const secretRefPrefix = "secret:"

// EnvEntry is one row of an application's environment as the API sees it: a
// plain KEY=VALUE pair, or a key whose value carries secretRefPrefix and names
// a sealed secret instead of its contents.
type EnvEntry struct {
	Key   string
	Value string
}

// CreateApplicationInput is the validated creation payload (the wire shape in
// routes.go mirrors it one to one).
type CreateApplicationInput struct {
	Name          string
	EnvironmentID uuid.UUID
	Provider      string
	Repo          string
	CloneURL      string
	// SourceType names how the application fetches its code (GS-2); empty
	// normalizes from Provider so pre-GS-2 clients keep their behaviour.
	SourceType string
	// GitHubAppID links the application to its GitHub App connection (GS-5);
	// uuid.Nil leaves it unlinked. The connection must belong to the caller.
	GitHubAppID uuid.UUID
	// DockerfileContent holds pasted Dockerfile text for the dockerfile
	// source type (GS-7); ignored for every other type.
	DockerfileContent string
	// BuildArgs holds the optional --build-arg pairs for the dockerfile
	// source type; ignored for every other type.
	BuildArgs map[string]string
	Branch    string
	BuildPack string
	// ImageRef is the prebuilt reference for image sources (GS-9).
	ImageRef string
	// RegistryUsername and RegistryPassword are the optional private-registry
	// credential for image sources, plaintext on the way in and sealed at
	// rest. They are never returned by the API.
	RegistryUsername string
	RegistryPassword string
	// ComposeContent holds the pasted compose file text of a compose
	// application; ComposeFile the in-repo path of a repo-backed one (GS-8).
	// Exactly one of the two is set for compose sources; both stay empty for
	// every other type. ComposeService names the compose service the
	// application's domain/port routing targets.
	ComposeContent string
	ComposeFile    string
	ComposeService string
	BaseDomain     string
	Port           int32
	HostPort       int32
	ServerID       uuid.UUID
	Env            []EnvEntry
	Storage        []Storage
}

// UpdateApplicationInput carries the mutable application fields. Every field is
// optional (a zero value means "leave unchanged"). ServerID changes the node
// (refused while a deployment runs); it must name a server, clearing it is
// rejected since PE-2 made the assignment required. EnvironmentID moves the
// application to another environment of the same team.
type UpdateApplicationInput struct {
	Name      *string
	Branch    *string
	BuildPack *string
	// ImageRef replaces the prebuilt reference of an image source (GS-9).
	ImageRef *string
	// RegistryUsername and RegistryPassword rotate the private-registry
	// credential of an image source; either may be set independently, and an
	// empty value clears that half. Plaintext on the way in, sealed at rest,
	// never returned by the API.
	RegistryUsername *string
	RegistryPassword *string
	BaseDomain       *string
	Port             *int32
	HostPort         *int32
	ServerID         *uuid.UUID
	EnvironmentID    *uuid.UUID
	// GitHubAppID relinks the application: a value sets the connection (it
	// must belong to the caller), an empty string clears it, absent leaves
	// it unchanged.
	GitHubAppID *string
	// DockerfileContent replaces the stored Dockerfile text for dockerfile
	// applications (validated like creation); BuildArgs replaces the whole
	// --build-arg collection (nil leaves it unchanged).
	DockerfileContent *string
	BuildArgs         *map[string]string
	// ComposeContent replaces the stored compose file text of a compose
	// application (setting it switches a repo-backed one to pasted mode and
	// clears its repository); ComposeFile replaces the in-repo path (setting
	// it needs a repository); ComposeService replaces the routed web service.
	// Nil leaves the field unchanged.
	ComposeContent *string
	ComposeFile    *string
	ComposeService *string
}

// ApplicationFilter scopes a list to one environment or one project of the
// caller's team (the ?environment_id= and ?project_id= filters). At most one
// may be set; a foreign ID answers ErrNotFound. IncludePreviews lists
// preview siblings too (the resources endpoint's ?previews=1); the default
// listing hides them.
type ApplicationFilter struct {
	EnvironmentID   uuid.UUID
	ProjectID       uuid.UUID
	IncludePreviews bool
}

// empty reports whether the update carries no field at all.
func (in UpdateApplicationInput) empty() bool {
	return in.Name == nil && in.Branch == nil && in.BuildPack == nil &&
		in.ImageRef == nil && in.RegistryUsername == nil && in.RegistryPassword == nil &&
		in.BaseDomain == nil && in.Port == nil && in.HostPort == nil &&
		in.ServerID == nil && in.EnvironmentID == nil && in.GitHubAppID == nil &&
		in.DockerfileContent == nil && in.BuildArgs == nil &&
		in.ComposeContent == nil && in.ComposeFile == nil && in.ComposeService == nil
}

// CreateApplication validates and stores a new application together with its
// environment and storage configuration. It answers only once every row of the
// payload is committed.
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
		ID:            uuid.New(),
		UserID:        userID,
		TeamID:        teamID,
		EnvironmentID: in.EnvironmentID,
		Name:          strings.TrimSpace(in.Name),
		Provider:      strings.TrimSpace(in.Provider),
		Repo:          strings.TrimSpace(in.Repo),
		CloneURL:      strings.TrimSpace(in.CloneURL),
		SourceType:    NormalizeSourceType(strings.TrimSpace(in.SourceType), strings.TrimSpace(in.Provider)),
		Branch:        strings.TrimSpace(in.Branch),
		BuildPack:     strings.TrimSpace(in.BuildPack),
		ImageRef:      strings.TrimSpace(in.ImageRef),
		// The username is an identifier, not a secret, but it is equally
		// never returned by the API or logged; the password is sealed below.
		// Both are validated together: a half credential is refused.
		RegistryUsername:  strings.TrimSpace(in.RegistryUsername),
		ComposeContent:    in.ComposeContent,
		ComposeFile:       strings.TrimSpace(in.ComposeFile),
		ComposeService:    strings.TrimSpace(in.ComposeService),
		BaseDomain:        proxy.NormalizeDomain(in.BaseDomain),
		Port:              in.Port,
		HostPort:          in.HostPort,
		ServerID:          in.ServerID,
		GitHubAppID:       in.GitHubAppID,
		DockerfileContent: in.DockerfileContent,
		BuildArgs:         normalizeBuildArgs(in.BuildArgs),
	}
	registryPassword := strings.TrimSpace(in.RegistryPassword)
	if err := validateRegistryCredential(app.RegistryUsername, registryPassword); err != nil {
		return Application{}, err
	}
	if registryPassword != "" {
		sealed, err := providers.SealSecret(s.secret, registryPassword)
		if err != nil {
			return Application{}, fmt.Errorf("deploy: seal registry credential: %w", err)
		}
		app.RegistryPasswordCiphertext = sealed
	}
	// A dockerfile application carries no repository: the pasted text is the
	// whole build context, so any repo fields a direct API caller sent are
	// cleared rather than stored as stale rows the build silently ignores.
	// Build args are validated raw before normalization, exactly like the
	// update path, so both write paths accept the same payloads.
	if app.SourceType == SourceDockerfile {
		app.Repo = ""
		app.CloneURL = ""
		if err := ValidateBuildArgs(in.BuildArgs); err != nil {
			return Application{}, err
		}
	}
	// A pasted compose document is the whole source: any repo fields a direct
	// API caller sent are cleared rather than stored as stale rows the deploy
	// silently ignores. Compose applications never use build-pack detection.
	if app.SourceType == SourceCompose {
		app.BuildPack = ""
		if strings.TrimSpace(app.ComposeContent) != "" {
			app.Repo = ""
			app.CloneURL = ""
			app.ComposeFile = ""
		}
	}
	// An empty branch stays empty for public-git sources: the clone resolves
	// the remote default via ls-remote (GS-3) instead of guessing "main".
	// The branch column is NOT NULL DEFAULT 'main', which only fills rows
	// that omit the column; storing '' explicitly is allowed and round-trips
	// unchanged. Provider flows keep the "main" fallback (their wizard
	// always prefills a branch, and empty would break push-branch matching).
	// Image sources carry no branch at all.
	if app.Branch == "" && !BranchDefaultsToRemote(app.SourceType, app.Provider) &&
		NormalizeSourceType(app.SourceType, app.Provider) != SourceImage {
		app.Branch = defaultBranch
	}
	// A pasted compose document has no branch: the provider fallback above
	// must not fill one in.
	if app.SourceType == SourceCompose && strings.TrimSpace(app.ComposeContent) != "" {
		app.Branch = ""
	}
	if err := validateApplication(app, true); err != nil {
		return Application{}, err
	}
	// A link names a GitHub App connection the caller must own; a foreign
	// id answers not-found, like any other foreign resource.
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
	storages, err := normalizeStorages(app.ID, in.Storage)
	if err != nil {
		return Application{}, err
	}
	created, err := s.repo.CreateApplication(ctx, app, envVars, secrets, storages)
	if err != nil {
		return Application{}, err
	}
	// The domain rows (JUS-89) mirror the primary: a failure here removes the
	// just-created row so no application exists without its routing state.
	if err := s.reconcilePrimaryRow(ctx, created.ID, created.BaseDomain); err != nil {
		if deleteErr := s.repo.DeleteApplication(ctx, created.ID); deleteErr != nil {
			s.logger.Warn("deploy: could not remove application after domain row failure",
				"application_id", created.ID, "error", deleteErr)
		}
		return Application{}, err
	}
	// A new domain starts routing as soon as it is stored; the node may not
	// have Traefik yet, in which case the sync bootstraps it.
	if created.BaseDomain != "" {
		s.syncProxy(ctx, created)
	}
	return created, nil
}

// InstallHook installs the provider hook that triggers automatic deploys of
// a freshly created application (BE-4.4). It is deliberately best effort:
// the application row is already committed, so a provider outage, missing
// credentials or an unusable callback origin must not fail the create and
// leave the caller with an application it does not know exists. The call is
// bounded by Config.HookTimeout — a provider that accepts the connection and
// then stalls must not hold the create request past the SPA's own timeout —
// and a failure is logged with the configured logger (never silently
// dropped). The explicit idempotent POST /v1/applications/{id}/webhooks route
// is the retry path.
//
// attempted is false when no hook lifecycle is wired: there is no outcome to
// report and the create response omits the webhook field.
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

// ListApplications returns the active team's applications, newest first.
// Without a team context it returns the creator's applications, which is the
// pre-teams behavior. A filter scopes the list to one environment or project
// of the caller's team (a foreign ID answers ErrNotFound).
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

// GetApplication returns one application the caller owns. Another user's row
// answers ErrNotFound, so application IDs cannot be probed.
func (s *Service) GetApplication(ctx context.Context, userID, appID uuid.UUID) (Application, error) {
	return s.application(ctx, userID, appID, false)
}

// UpdateApplication applies a partial update to the mutable application fields
// (name, branch, build pack, domain, ports, server).
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
	// Serialize with deployment submission and manual control: a move may stop
	// the container on the previous node, and that must not race a deploy that
	// is selecting or retiring a target. Re-read under the lock so the update
	// is applied to the current row (no lost update from a stale snapshot).
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return Application{}, err
	}
	// A move changes the node an in-flight deployment would target; refuse it
	// while a deployment is non-terminal, exactly like a manual control or a
	// delete. The worker does not take this lock, so the guard is what keeps a
	// move from stranding a container on the previous node.
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
	// A move to another environment stays in the team (a foreign environment
	// answers 404) and refuses a name the target already holds (409), per the
	// per-environment uniqueness.
	movedEnvironment := false
	if in.EnvironmentID != nil && *in.EnvironmentID != app.EnvironmentID {
		// Open previews stay pinned to the old environment and node, so a
		// base with live previews cannot move until they are closed.
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
	previousDomain, previousPort, previousHostPort := app.BaseDomain, app.Port, app.HostPort
	if in.Name != nil {
		app.Name = strings.TrimSpace(*in.Name)
	}
	// Image sources carry no branch or build pack; the reference and the
	// private-registry credential are their only source fields. Neither
	// half is ever returned by the API.
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
		// The credential is bound to the registry host it was entered for:
		// moving the reference to another host without re-entering the
		// password would send the stored secret to that host on the next
		// pull, so the update is refused until the password is re-entered
		// (or the credential is cleared explicitly in the same request).
		hostChanged := RegistryHost(app.ImageRef) != RegistryHost(ref)
		if hostChanged && app.RegistryPasswordCiphertext != "" && in.RegistryPassword == nil {
			return Application{}, fmt.Errorf("%w: image reference moves to another registry host: re-enter the registry password or clear the credential",
				ErrValidation)
		}
		if hostChanged && in.RegistryUsername == nil {
			// The username is half of the bound credential: it must not
			// follow the reference to the new host. Dropping it here forces
			// the writer to supply both halves with the password (or clear
			// both), per the pair rule below.
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
	// The halves rotate independently, but the stored pair must stay whole:
	// a username without a password (or the reverse) would pull anonymously
	// while reporting a credential as configured.
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
	if in.BaseDomain != nil {
		next := proxy.NormalizeDomain(*in.BaseDomain)
		// Only an actual domain change resolves a migration-disabled binding:
		// a full-form update that resends the unchanged value (however
		// cased) must not silently reactivate a legacy conflict. The stored
		// value is normalized either way (BE-6.1 F6/F8).
		if next != proxy.NormalizeDomain(app.BaseDomain) {
			app.BaseDomainDisabled = false
		}
		app.BaseDomain = next
	}
	if in.Port != nil {
		app.Port = *in.Port
	}
	if in.HostPort != nil {
		app.HostPort = *in.HostPort
	}
	// Compose fields apply after the port updates: the compose validation
	// reads the effective container port.
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
	// Relinking follows the create rules: a value sets the connection (it
	// must belong to the caller), an empty string clears it, absent leaves
	// it unchanged. The link only makes sense on the github_app source.
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
		// Relinking an http clone URL would embed the next installation
		// token in plaintext: refuse, like creation does.
		if err := validateLinkedCloneURL(app); err != nil {
			return Application{}, err
		}
	}
	// Clearing the branch restores ls-remote default resolution for
	// public-git sources (see the create path); provider flows fall back to
	// "main" so push-branch matching keeps working. Image sources carry no
	// branch at all, and a pasted compose document has none either (cleared
	// below).
	if app.Branch == "" && !BranchDefaultsToRemote(app.SourceType, app.Provider) &&
		NormalizeSourceType(app.SourceType, app.Provider) != SourceImage {
		app.Branch = defaultBranch
	}
	if app.SourceType == SourceCompose && strings.TrimSpace(app.ComposeContent) != "" {
		app.Branch = ""
	}
	// Legacy rows may predate normalization: normalize the resulting value so
	// an unrelated update (rename, branch) never fails on stored casing, even
	// with FEATURE_PROXY=false (BE-6.1 F8). An explicitly changed domain is
	// still validated strictly below.
	app.BaseDomain = proxy.NormalizeDomain(app.BaseDomain)
	// A port-only update on a compose application must keep the deploy
	// target valid: the compose gate reads the effective row.
	if isComposeApp(app) {
		if err := validateComposeTarget(app); err != nil {
			return Application{}, err
		}
	}
	// The clone URL is not part of the update payload, so it is left out of
	// validation: an application created with a development-local source must
	// still be renameable.
	if err := validateApplication(app, false); err != nil {
		return Application{}, err
	}
	// A base_domain write reconciles the domain rows first: the rows enforce
	// the platform-wide uniqueness, so a conflicting host fails here before
	// the application row is touched. Unrelated updates skip the rows
	// entirely.
	if in.BaseDomain != nil {
		if err := s.reconcilePrimaryRow(ctx, app.ID, app.BaseDomain); err != nil {
			return Application{}, err
		}
	}
	updated, err := s.repo.UpdateApplication(ctx, app)
	if err != nil {
		// A concurrent create or move that committed past the pre-check trips
		// the unique index instead: still a 409, with the move message.
		if movedEnvironment && errors.Is(err, ErrValidation) &&
			strings.Contains(err.Error(), "already exists") {
			return Application{}, fmt.Errorf("%w: an application named %q already exists in the target environment",
				ErrNameConflict, app.Name)
		}
		return Application{}, err
	}
	// Routing input changed: refresh the hosting node's configuration. A node
	// whose route may have disappeared (a move, a changed domain, a cleared
	// domain) is refreshed too, so no stale route is left behind.
	moved := updated.ServerID != previousServer
	if moved {
		// The application is now bound to another node; stop the container it
		// left behind on the previous one (best effort — an unreachable node
		// must not fail the move). Deployments do not record their node, so the
		// newest recorded container is the one that ran there. A compose
		// application also leaves a whole project behind: down removes the
		// sidecars the container sweep cannot see.
		s.stopContainerOnNode(ctx, previousServer, s.latestContainer(ctx, app.ID))
		s.composeDownApp(ctx, previousServer, updated)
	}
	routingChanged := updated.BaseDomain != previousDomain || updated.Port != previousPort ||
		updated.HostPort != previousHostPort
	targets := make(map[uuid.UUID]bool, 2)
	if updated.BaseDomain != "" && (routingChanged || moved) {
		targets[updated.ServerID] = true
	}
	if previousDomain != "" && (moved || updated.BaseDomain != previousDomain) {
		targets[previousServer] = true
	}
	for serverID := range targets {
		s.syncProxyServer(ctx, serverID)
	}
	return updated, nil
}

// DeleteApplication removes an application together with everything that hangs
// off it. An application with a non-terminal deployment is refused with
// ErrConflict: deleting it would cascade the deployment row while the worker
// may be mid-run, orphaning a container no later deploy or delete can reach.
// Its provider hook is detached first and the delete fails closed on a
// removal failure, so nothing that would break the still-live application has
// been mutated when it aborts (the escape hatch for a hook whose provider is
// gone is ForgetWebhook, DELETE .../webhooks?force=true). The deploy key is
// detached next, best effort: once the hook is gone, a host failure must not
// leave a live application without automatic deploys (the local row cascades
// with the application and the warning names the remote key). The container
// removal stays best effort too: a control plane that cannot reach the node
// must still be able to delete an application.
func (s *Service) DeleteApplication(ctx context.Context, userID, appID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	// Serialize with deployment submission and manual control: the in-flight
	// check and the cascade must be atomic with respect to a new deployment.
	// Re-read under the lock so a concurrent move does not leave the delete
	// removing containers on the previous node and missing the current one.
	unlock := s.locks.lock(app.ID)
	defer unlock()
	app, err = s.application(ctx, userID, appID, true)
	if err != nil {
		return err
	}
	if err := s.rejectInFlight(ctx, app.ID); err != nil {
		return err
	}
	// Preview siblings hang off this application outside the deploy schema
	// (preview_deploys is keyed by the base application and cascades); they
	// must be torn down before the base row disappears, or the cascade would
	// erase the only record of them. A cleanup failure aborts the delete: the
	// container stop inside the teardown is best effort, but the durable
	// binding must not be lost while a sibling still exists.
	if s.previewCleanup != nil {
		if err := s.previewCleanup(ctx, app.ID); err != nil {
			return err
		}
	}
	// Remove the provider hook (BE-4.4) FIRST, bounded like the create call,
	// and fail closed: the stored row is the only handle on the remote hook,
	// so deleting it while the host may still hold the hook would orphan the
	// hook with nothing left to identify it (the explicit route could no
	// longer reach it). Ordering matters for the still-live application too:
	// when this returns an error, the deploy key, the container and the row
	// are all untouched, so a webhook deploy that still reaches the app can
	// keep cloning. The caller retries, removes the hook on the host by hand,
	// or acknowledges the orphan with
	// DELETE /v1/applications/{id}/webhooks?force=true (ForgetWebhook) and
	// deletes again. A preview sibling never holds its own hook.
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
	// Detach the deploy key next. It is BEST EFFORT once the provider hook is
	// gone: aborting here would leave a live application whose automatic
	// deploys are silently disabled — the exact state this ordering exists to
	// prevent. The local key row cascades away with the application; a key
	// left registered on the Git host is the lesser evil, and the warning
	// names provider and repository so it can be removed there by hand. A
	// preview sibling reuses its base application's remote deploy key, so
	// only its local rows go and the remote key always stays registered.
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
	// Containers are removed before the row: the row is the only durable record
	// of the application's containers, so a row-delete failure after a
	// successful removal leaves no container behind (the accepted trade; the
	// caller retries the delete). A container left on a previous node by a move
	// is out of reach here — the move stopped it best effort and deployments do
	// not record their node (see the report). A compose project is torn down
	// first for the same reason: its sidecars survive the container sweep.
	s.composeDownApp(ctx, app.ServerID, app)
	s.removeApplicationContainers(ctx, app)
	if err := s.repo.DeleteApplication(ctx, appID); err != nil {
		return err
	}
	// The application row is gone: regenerate the node's configuration so the
	// deleted route stops being served immediately. An application without a
	// domain never had a route to remove.
	if app.BaseDomain != "" {
		s.syncProxyServer(ctx, app.ServerID)
	}
	return nil
}

// GetEnv returns the application's environment: plain values verbatim and
// sealed secrets as `secret:<id>` references. Plaintext is never returned.
func (s *Service) GetEnv(ctx context.Context, userID, appID uuid.UUID) ([]EnvEntry, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	return s.envEntries(ctx, appID)
}

// ReplaceEnv rewrites the whole environment collection and returns it as
// stored. A `secret:` value that still points at a stored secret keeps its
// ciphertext; any other `secret:` value is sealed as new plaintext.
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

// GetStorages returns the application's storage mappings.
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

// ReplaceStorages rewrites the whole storage collection and returns it as
// stored.
func (s *Service) ReplaceStorages(ctx context.Context, userID, appID uuid.UUID, storages []Storage) ([]Storage, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	if _, err := s.application(ctx, userID, appID, true); err != nil {
		return nil, err
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

// warnStorageChanges logs when a save changes the resolved host source of an
// existing storage row in a way that strands data: a previously explicit host
// path blanked to a managed path, or a pre-change bare named volume silently
// re-namespaced. An intentional explicit change is left to the operator, and a
// re-save that resolves to the same value (the derived managed path) does not
// warn.
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

// Stop stops the container of the application's newest deployment.
func (s *Service) Stop(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	return s.controlContainer(ctx, userID, appID, false)
}

// Start restarts the container of the application's newest deployment.
func (s *Service) Start(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	return s.controlContainer(ctx, userID, appID, true)
}

// controlContainer runs one manual container operation through the node seam.
// The deployment row is left untouched: it identifies the release, and its
// state must keep describing that release (a stop would otherwise break
// rollback target selection). The agent call is bounded by ControlTimeout; a
// call that exhausts the bound answers ErrAgentUnavailable, which the client
// may safely retry (start/stop are idempotent).
func (s *Service) controlContainer(ctx context.Context, userID, appID uuid.UUID, start bool) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	// Hold the application lock across target selection and the agent call: a
	// concurrent deploy must not retire the selected container between the
	// in-flight check and a delayed start/stop, which would restart a retired
	// release (leaving two running). Re-read under the lock so a concurrent
	// move cannot leave the manual call acting on the previous node.
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
	// Bound the agent call so a hung agent cannot pin the application lock and
	// stall every deploy, move and delete for this application.
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

// controlTarget picks the container a manual stop/start acts on: the newest
// deployment that started one. An in-flight deployment answers ErrConflict (its
// container is being replaced right now), and an application that never
// started a container answers ErrNotFound.
func (s *Service) controlTarget(ctx context.Context, app Application) (Deployment, error) {
	if app.ServerID == uuid.Nil {
		return Deployment{}, ErrServerNotFound
	}
	deployments, err := s.repo.ListDeployments(ctx, app.ID)
	if err != nil {
		return Deployment{}, err
	}
	for _, dep := range deployments { // newest first
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

// rejectInFlight returns ErrConflict when the application has a non-terminal
// deployment: its container is being replaced right now, so a manual control
// or a delete must wait for it to finish. It is the delete-side mirror of
// controlTarget's guard.
func (s *Service) rejectInFlight(ctx context.Context, appID uuid.UUID) error {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		return err
	}
	for _, dep := range deployments { // newest first
		if !dep.State.Terminal() {
			return fmt.Errorf("%w: a deployment is in progress", ErrConflict)
		}
	}
	return nil
}

// latestContainer returns the newest container any deployment of the
// application started ("" when there is none).
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

// containerCleanupTimeout bounds one best-effort container operation on a node
// that may have stopped answering. It is a safety net so a hung node cannot pin
// the application lock; it is not a retry budget.
const containerCleanupTimeout = 15 * time.Second

// composeCleanupTimeout bounds one best-effort compose teardown (resolving
// the document, rendering it and running down on the node). A repo-backed
// teardown clones first, so the budget covers a slow Git host; it is not a
// retry budget.
const composeCleanupTimeout = 2 * time.Minute

// composeDownApp tears a compose application's project down on one node,
// swallowing every failure: a delete or a move must not depend on the node
// being reachable. The project's sidecars carry no gotham labels for the
// container sweep to find, so down (which only ever touches this project's
// containers and never its volumes) is what removes them.
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
		// The stored document no longer resolves: fail loudly (an error,
		// not a debug) and still tear the project down with a minimal
		// document, so the sidecars are never silently left behind.
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
	// Teardown stays best effort: the probe never aborts it, but a node
	// that does not report enforcement is logged loudly, so a fleet with
	// old agents is visible instead of silently unconfined.
	if err := s.probeComposeConfinement(opCtx, node, app); err != nil {
		s.logger.Warn("deploy: compose teardown proceeds without verified confinement",
			"application_id", app.ID, "error", err)
	}
	if err := node.ComposeDown(opCtx, services.ProjectName(app.ID), []byte(content)); err != nil {
		s.logger.Warn("deploy: best-effort compose teardown failed", "application_id", app.ID, "error", err)
	}
}

// stopContainerOnNode stops one container on one node, swallowing every
// failure: a move must not depend on the previous node being reachable.
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

// removeApplicationContainers removes every container of an application from
// its node, swallowing every failure: deletion must not depend on the node
// being reachable. Containers are found by the gotham.app_id label so a
// container orphaned by a failed deploy or a lost response is reached too; the
// recorded deployment container IDs are removed as well, which also covers a
// node that cannot list containers. Rollback images and persistent bind
// directories survive (the agent's Remove removes only the container and its
// anonymous volumes).
func (s *Service) removeApplicationContainers(ctx context.Context, app Application) {
	if app.ServerID == uuid.Nil || s.dial == nil {
		return
	}
	// Delete spells no requirement on the node answering; bound every call so a
	// hung node cannot pin the application lock.
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

// teamIDFor resolves the team a resource call operates in: the request's
// active team, or the caller's personal team when no team context is present
// (the pre-teams path), matching the projects surface.
func teamIDFor(ctx context.Context, userID uuid.UUID) uuid.UUID {
	scope := teams.ScopeFor(ctx, userID)
	if scope.Active() {
		return scope.TeamID
	}
	return teams.PersonalTeamID(userID)
}

// validateServer enforces that the application points at a server the control
// plane knows AND that the caller's active team may target it. A node of
// another team fails like a missing one (ErrServerNotFound, 404), so node IDs
// cannot be probed and an application can never be bound to a foreign team's
// node (F6). A legacy node without a team stays shared.
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

// applyComposeUpdate applies the compose field updates to a compose
// application. Setting content switches a repo-backed application to pasted
// mode and clears its repository (the pasted document is the whole source);
// setting a file path needs the repository the path lives in. The web
// service is validated against the effective document when one is available
// (pasted content, new or stored); repo-backed applications check membership
// at deploy time, once the file is read from the checkout.
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

// validateApplication checks the fields every write path shares: a name, a
// cloneable source (creation only), a known source type with its per-type
// fields, a build pack builds.ParseEngineKind
// accepts ("" selects auto-detection) and ports inside the 0–65535 range the
// schema CHECKs enforce.
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
	if app.BaseDomain != "" {
		// The domain ends up inside a Traefik Host() rule, so it is validated
		// at write time: an injection attempt or a malformed hostname must
		// never reach the generator.
		if err := proxy.ValidateDomain(app.BaseDomain); err != nil {
			return fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}
	if err := validatePort("port", app.Port); err != nil {
		return err
	}
	return validatePort("host port", app.HostPort)
}

// Registry credential bounds: a username is an identifier, a password or
// token is sealed at rest. Halves are refused on every write path, so a
// stored credential is always a usable pair.
const (
	maxRegistryUsernameLen = 255
	maxRegistryPasswordLen = 4096
)

// validateRegistryCredential checks a plaintext credential pair: both or
// neither, within the length caps. The error never carries the values.
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

// requireCompleteRegistryCredential refuses a half credential: a username
// without a password would pull anonymously while the API reports a
// credential as configured (and the reverse stores a secret no pull uses).
func requireCompleteRegistryCredential(username string, hasPassword bool) error {
	hasUser := username != ""
	if (hasUser && !hasPassword) || (!hasUser && hasPassword) {
		return fmt.Errorf("%w: registry credential needs both a username and a password", ErrValidation)
	}
	return nil
}

// validateSource checks the per-type source fields on creation. The provider
// slug must agree with the type (github_app needs provider "github",
// gitlab_app "gitlab"), so hook and deploy-key paths that key off Provider
// can never disagree with the orchestrator switch that keys off SourceType.
// git_public refuses only the two slugs that own a dedicated type and takes a
// keyless http(s)/git URL; every other provider value ("", the legacy
// "public" sentinel, gitea and friends) stays git_public, so existing apps
// and their previews keep working with the provider-based hook and key
// behavior they already have. git_private is provider-less by definition (a
// private repository on a connected provider uses that provider's type) and
// takes an ssh/scp-like or https URL. The dockerfile type needs pasted
// content (validated without executing anything) plus optional build args
// and no repository. Image sources (GS-9) carry only a validated reference
// and an optional credential — no repository, branch or build pack. The
// compose type (GS-8) takes either pasted content or a repository with an
// in-repo file path, plus the routed web service. Image fields on any other
// type are refused, so a credential can never be stored where no pull reads
// it.
func validateSource(app Application) error {
	// A GitHub App link only makes sense on the github_app source: anything
	// else never consults it, so linking there is a caller error.
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

// composePrivilegedPortEnd is the end of the privileged host-port band.
// Compose applications bind their web service's published port on the
// node, and the proxy routes any host port to it — but ports below 1024
// belong to the node itself (Traefik, SSH, the registry) and to other
// tenants' mappings. A compose application pins HostPort 0 (an ephemeral
// port the proxy discovers) or a high port; ordinary single-container
// applications keep their existing range.
const composePrivilegedPortEnd = 1023

// validateComposePorts checks the application's port pair: a real container
// port for the web service, and a host port that is either auto-assigned
// or outside the privileged band.
func validateComposePorts(port, hostPort int32) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: compose source needs a container port between 1 and 65535", ErrValidation)
	}
	if hostPort != 0 && (hostPort < 1 || hostPort > 65535) {
		return fmt.Errorf("%w: host port must be between 0 and 65535", ErrValidation)
	}
	if hostPort >= 1 && hostPort <= composePrivilegedPortEnd {
		return fmt.Errorf("%w: compose source must not pin privileged host port %d (use 0 for auto-assign or a port above %d)",
			ErrValidation, hostPort, composePrivilegedPortEnd)
	}
	return nil
}

// validateComposeSource checks a compose application: exactly one of pasted
// content or a repository with an in-repo file path, plus the routed web
// service. A pasted document carries no repository and is validated whole
// (structure, application scope, service membership); a repo-backed one
// reuses the matching git flow's repository rules and checks membership at
// deploy time, once the file is read from the checkout.
func validateComposeSource(app Application) error {
	pasted := strings.TrimSpace(app.ComposeContent) != ""
	repo := strings.TrimSpace(app.Repo) != "" || strings.TrimSpace(app.CloneURL) != "" ||
		strings.TrimSpace(app.ComposeFile) != ""
	if pasted == repo {
		return fmt.Errorf("%w: compose source needs either pasted content or a repository with a compose file, not both",
			ErrValidation)
	}
	if err := validateComposePorts(app.Port, app.HostPort); err != nil {
		return err
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

// validateLinkedCloneURL requires a linked application to carry an https
// clone URL when it carries a URL with a scheme: an installation token must
// never travel over plaintext http. SSH/scp-like URLs carry no scheme and
// never take the token path, so they are unaffected.
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

// validateProviderSource checks a provider-backed source: the slug must match
// the type, and the repository and clone URL follow the existing flow.
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

// validatePort rejects values the applications table CHECKs would refuse, so a
// bad port is a 400 instead of a database error.
func validatePort(field string, port int32) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("%w: %s must be between 0 and 65535", ErrValidation, field)
	}
	return nil
}

// prepareEnv turns API entries into the two collections that are stored: plain
// env vars and sealed secrets. Values carrying secretRefPrefix are sealed with
// providers.SealSecret — unless they still reference a secret this application
// already holds, in which case the ciphertext is re-written unchanged so a
// configuration round-trip cannot re-seal a reference as if it were plaintext.
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
		// A reference the application does not hold is a stale or invented one:
		// re-sealing it would silently replace the secret with a UUID.
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

// validateEnvKey accepts any structurally possible container variable name.
// The FE warns (without blocking) on ^[A-Z][A-Z0-9_]*$; the API only rejects
// what Docker could not pass through.
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

// normalizeStorages trims and validates the volume map: named rows with a
// container path and either a managed host path, a host bind confined to
// <managed root>/<appID>, or a Docker named volume. An empty host path is
// derived to a managed directory, so a client never needs to know the
// application id up front. The rules mirror volumeSpecs, so a bad mapping is a
// 400 at write time rather than a failed deployment later.
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
		// Two distinct names that sanitize to the same directory would share
		// one host path once derived; reject rather than silently fuse them.
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
			// Named volumes are namespaced with the storage name; reject two
			// rows whose (sanitized) names collapse onto one volume.
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

// envEntries merges the stored plain env vars and sealed secrets into the API
// view of the environment, sorted by key. Secrets appear as their reference —
// the ciphertext and the plaintext stay on the server.
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
