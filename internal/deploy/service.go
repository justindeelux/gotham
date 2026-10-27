package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/runtimewatch"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/taskevents"
)

const FeatureEnv = "FEATURE_APPLICATIONS"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const DefaultHookTimeout = 8 * time.Second

type DeployService interface {
	GetApplicationLogs(context.Context, uuid.UUID, uuid.UUID, LogOptions) (ApplicationLogs, error)
	GetDeploymentLogs(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, LogOptions) (DeploymentLogs, error)
	GetApplicationRuntime(ctx context.Context, userID, appID uuid.UUID) (ApplicationRuntime, error)
	CreateApplication(ctx context.Context, userID uuid.UUID, in CreateApplicationInput) (Application, error)
	InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) (attempted bool, err error)
	ListApplications(ctx context.Context, userID uuid.UUID, filter ApplicationFilter) ([]Application, error)
	GetApplication(ctx context.Context, userID, appID uuid.UUID) (Application, error)
	UpdateApplication(ctx context.Context, userID, appID uuid.UUID, in UpdateApplicationInput) (Application, error)
	ListDomains(ctx context.Context, userID, appID uuid.UUID) ([]ApplicationDomain, error)
	AddDomain(ctx context.Context, userID, appID uuid.UUID, in DomainInput) (ApplicationDomain, error)
	UpdateDomain(ctx context.Context, userID, appID, domainID uuid.UUID, in DomainInput) (ApplicationDomain, error)
	RemoveDomain(ctx context.Context, userID, appID, domainID uuid.UUID) error
	SetPrimaryDomain(ctx context.Context, userID, appID, domainID uuid.UUID) error
	DeleteApplication(ctx context.Context, userID, appID uuid.UUID) error
	GetEnv(ctx context.Context, userID, appID uuid.UUID) ([]EnvEntry, error)
	ReplaceEnv(ctx context.Context, userID, appID uuid.UUID, entries []EnvEntry) ([]EnvEntry, error)
	GetStorages(ctx context.Context, userID, appID uuid.UUID) ([]Storage, error)
	ListVolumes(ctx context.Context, userID, appID uuid.UUID) (VolumeListing, error)
	ReplaceStorages(ctx context.Context, userID, appID uuid.UUID, storages []Storage) ([]Storage, error)
	Stop(ctx context.Context, userID, appID uuid.UUID) (Deployment, error)
	Start(ctx context.Context, userID, appID uuid.UUID) (Deployment, error)
	CreateDeployKey(ctx context.Context, userID, appID uuid.UUID) (DeployKey, error)
	GetDeployKey(ctx context.Context, userID, appID uuid.UUID) (DeployKey, error)
	SetGitCredential(ctx context.Context, userID, appID uuid.UUID, username, token string) (GitCredentialState, error)
	GetGitCredential(ctx context.Context, userID, appID uuid.UUID) (GitCredentialState, error)
	DeleteGitCredential(ctx context.Context, userID, appID uuid.UUID) (bool, error)
	TestGitConnection(ctx context.Context, userID, appID uuid.UUID) (GitConnectionResult, error)
	DeleteDeployKey(ctx context.Context, userID, appID uuid.UUID) (bool, error)
	Deploy(ctx context.Context, userID, appID uuid.UUID) (Deployment, error)
	ListDeployments(ctx context.Context, userID, appID uuid.UUID) ([]Deployment, error)
	ListDeploymentsLimit(ctx context.Context, userID, appID uuid.UUID, limit int) ([]Deployment, error)
	ListRecentDeployments(ctx context.Context, userID uuid.UUID, limit int) ([]ApplicationDeployment, error)
	GetDeployment(ctx context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error)

	GetDeploymentBuildLog(ctx context.Context, userID, appID, deploymentID uuid.UUID) (string, error)
	Rollback(ctx context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error)
}

type HookLifecycle interface {
	InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) error
	RemoveHook(ctx context.Context, userID, appID uuid.UUID) error
}

type ToolStatus interface {
	InstalledToolIDs(ctx context.Context, serverID uuid.UUID) (map[string]bool, error)
}

type Config struct {
	GuardDomains         proxy.DomainGuard
	Store                *store.Store
	Repository           Repository
	Secret               string
	RedisAddr            string
	Publisher            Publisher
	Dial                 DialFunc
	KeyRegistrar         KeyRegistrar
	Proxy                ProxySync
	TLSForDomains        func(context.Context) bool
	Notifier             Notifier
	Source               Source
	GitAllowPrivateHosts bool
	GitLookupHost        gitHostLookupFunc
	AppTokens            appTokenResolver
	PreviewCleanup       func(ctx context.Context, appID uuid.UUID) error
	Hooks                func() HookLifecycle
	HookTimeout          time.Duration
	Emitter              *Emitter
	Tasks                *taskevents.Tracker
	Logger               *slog.Logger
	Workers              int
	QueueSize            int
	MaxAttempts          int
	StepTimeout          time.Duration
	BuildTimeout         time.Duration
	HealthTimeout        time.Duration
	HealthPoll           time.Duration
	ControlTimeout       time.Duration
	ToolStatus           ToolStatus
	RuntimeMonitor       RuntimeMonitor
	RuntimeHealth        *runtimewatch.Reader
}

func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store, c.Secret)
	}
	return nil
}

type Service struct {
	guardDomains  proxy.DomainGuard
	tlsForDomains func(context.Context) bool
	*Orchestrator
	registrar      KeyRegistrar
	previewCleanup func(ctx context.Context, appID uuid.UUID) error
	hooks          func() HookLifecycle
	hookTimeout    time.Duration
	controlTimeout time.Duration
	runtimeMonitor RuntimeMonitor
	runtimeHealth  *runtimewatch.Reader
	locks          appLocks
	probeMu        sync.Mutex
	probeLast      map[probeThrottleKey]time.Time
}

type appLocks struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func (l *appLocks) lock(appID uuid.UUID) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[uuid.UUID]*sync.Mutex)
	}
	mu, ok := l.locks[appID]
	if !ok {
		mu = &sync.Mutex{}
		l.locks[appID] = mu
	}
	l.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

var _ DeployService = (*Service)(nil)

func NewService(cfg Config) *Service {
	o := newOrchestrator(cfg)
	o.recoverStale()
	hookTimeout := cfg.HookTimeout
	if hookTimeout <= 0 {
		hookTimeout = DefaultHookTimeout
	}
	controlTimeout := cfg.ControlTimeout
	if controlTimeout <= 0 {
		controlTimeout = containerCleanupTimeout
	}
	return &Service{
		guardDomains:   cfg.GuardDomains,
		tlsForDomains:  cfg.TLSForDomains,
		Orchestrator:   o,
		registrar:      cfg.KeyRegistrar,
		previewCleanup: cfg.PreviewCleanup,
		hooks:          cfg.Hooks,
		hookTimeout:    hookTimeout,
		controlTimeout: controlTimeout,
		runtimeMonitor: cfg.RuntimeMonitor,
		runtimeHealth:  cfg.RuntimeHealth,
	}
}

func (s *Service) hookLifecycle() HookLifecycle {
	if s == nil || s.hooks == nil {
		return nil
	}
	return s.hooks()
}

func (o *Orchestrator) recoverStale() {
	if o.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	n, err := o.repo.FailStaleDeployments(ctx)
	if err != nil {
		o.logger.Warn("deploy: stale deployment sweep failed", "error", err)
		return
	}
	if n > 0 {
		o.logger.Info("deploy: marked stale deployments failed", "count", n)
	}
}

func NewDefaultService(cfg Config) DeployService {
	if cfg.Store == nil && cfg.Repository == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return NewService(cfg)
}

func (s *Service) Close() error {
	err := s.Orchestrator.Close()
	if closer, ok := s.emitter.pub.(interface{ Close() error }); ok {
		if closeErr := closer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

func (s *Service) Deploy(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	if err := requestreplay.Fingerprint(ctx, "", struct{}{}); err != nil {
		return Deployment{}, err
	}
	if err := s.lookupReplay(ctx); err != nil {
		return Deployment{}, err
	}
	if result := requestreplay.ResultFrom(ctx); result != nil {
		if result.RelatedID != app.EnvironmentID {
			return Deployment{}, requestreplay.ErrMissing
		}
		return s.repo.GetDeployment(ctx, app.ID, result.ResourceID)
	}
	return s.deployApplication(ctx, app)
}

func (s *Service) DeploySystem(ctx context.Context, appID uuid.UUID) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	if s == nil || s.repo == nil {
		return Deployment{}, errors.New("deploy: repository is not configured")
	}
	if appID == uuid.Nil {
		return Deployment{}, fmt.Errorf("%w: invalid application id", ErrValidation)
	}
	app, err := s.repo.GetApplication(ctx, appID)
	if err != nil {
		return Deployment{}, err
	}
	return s.deployApplication(ctx, app)
}

func (s *Service) deployApplication(ctx context.Context, app Application) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	if err := validateDeployTarget(app); err != nil {
		return Deployment{}, err
	}
	if err := s.validateBuildToolchain(ctx, app); err != nil {
		return Deployment{}, err
	}
	return s.submit(ctx, app, Deployment{Kind: KindDeploy, State: StateQueued})
}

func (s *Service) validateBuildToolchain(ctx context.Context, app Application) error {
	if s.toolStatus == nil || app.ServerID == uuid.Nil {
		return nil
	}
	switch NormalizeSourceType(app.SourceType, app.Provider) {
	case SourceDockerfile, SourceCompose, SourceImage:
		return nil
	}
	kind, err := builds.ParseEngineKind(app.BuildPack)
	if err != nil {
		return nil
	}
	required := builds.EngineTools(kind)
	if len(required) == 0 {
		return nil
	}
	installed, err := s.toolStatus.InstalledToolIDs(ctx, app.ServerID)
	if err != nil {
		s.logger.Warn("deploy: build-pack gate lookup failed; allowing the deploy",
			"application_id", app.ID, "server_id", app.ServerID, "error", err)
		return nil
	}
	for _, toolID := range required {
		if !installed[toolID] {
			return fmt.Errorf("%w: build pack %q needs %q, which is not installed on the target node; install it from the App Store",
				ErrValidation, kind, toolID)
		}
	}
	return nil
}

func (s *Service) checkStoredTarget(ctx context.Context, app Application) error {
	if app.ServerID == uuid.Nil {
		return nil
	}
	known, err := s.repo.ServerExists(ctx, app.ServerID)
	if err != nil {
		return err
	}
	if !known {
		return ErrServerNotFound
	}
	return nil
}

func (s *Service) Rollback(ctx context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	target, err := s.rollbackTarget(ctx, app.ID, deploymentID)
	if err != nil {
		return Deployment{}, err
	}
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceCompose {
		if target.State != StateRunning {
			return Deployment{}, fmt.Errorf("%w: deployment %s is not a released compose deployment",
				ErrValidation, target.ID)
		}
		if strings.TrimSpace(target.ComposeDocument) == "" {
			return Deployment{}, fmt.Errorf("%w: deployment %s recorded no compose document to roll back to",
				ErrValidation, target.ID)
		}
		return s.submit(ctx, app, Deployment{
			Kind:            KindRollback,
			State:           StateQueued,
			ComposeDocument: target.ComposeDocument,
			ComposeCommit:   target.ComposeCommit,
			CommitSHA:       target.CommitSHA,
			CommitMessage:   target.CommitMessage,
			CommitAuthor:    target.CommitAuthor,
			CommittedAt:     target.CommittedAt,
			RollbackFrom:    target.ID,
		})
	}
	if target.State != StateRunning || strings.TrimSpace(target.ImageTag) == "" {
		return Deployment{}, fmt.Errorf("%w: deployment %s has no released image to roll back to",
			ErrValidation, target.ID)
	}
	registryImage := target.RegistryImage
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceImage {
		base := strings.TrimSpace(registryImage)
		if base == "" {
			base = strings.TrimSpace(target.ImageTag)
		}
		pinned, err := PinnedImageReference(base, target.Digest)
		if err != nil {
			return Deployment{}, err
		}
		registryImage = pinned
	}
	return s.submit(ctx, app, Deployment{
		Kind:          KindRollback,
		State:         StateQueued,
		ImageTag:      target.ImageTag,
		RegistryImage: registryImage,
		Digest:        target.Digest,
		CommitSHA:     target.CommitSHA,
		CommitMessage: target.CommitMessage,
		CommitAuthor:  target.CommitAuthor,
		CommittedAt:   target.CommittedAt,
		RollbackFrom:  target.ID,
	})
}

func (s *Service) ListDeployments(ctx context.Context, userID, appID uuid.UUID) ([]Deployment, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		return nil, err
	}
	if deployments == nil {
		return []Deployment{}, nil
	}
	return deployments, nil
}

const maxDeploymentListLimit = 100

func (s *Service) ListDeploymentsLimit(ctx context.Context, userID, appID uuid.UUID, limit int) ([]Deployment, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > maxDeploymentListLimit {
		limit = maxDeploymentListLimit
	}
	deployments, err := s.repo.ListDeploymentsLimit(ctx, appID, limit)
	if err != nil {
		return nil, err
	}
	if deployments == nil {
		return []Deployment{}, nil
	}
	return deployments, nil
}

func (s *Service) ListRecentDeployments(ctx context.Context, userID uuid.UUID, limit int) ([]ApplicationDeployment, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("deploy: repository is not configured")
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > maxDeploymentListLimit {
		limit = maxDeploymentListLimit
	}
	deployments, err := s.repo.ListRecentDeployments(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	if deployments == nil {
		return []ApplicationDeployment{}, nil
	}
	return deployments, nil
}

func (s *Service) GetDeploymentBuildLog(ctx context.Context, userID, appID, deploymentID uuid.UUID) (string, error) {
	if _, err := s.application(ctx, userID, appID, false); err != nil {
		return "", err
	}
	if deploymentID == uuid.Nil {
		return "", fmt.Errorf("%w: invalid deployment id", ErrValidation)
	}
	deployment, err := s.repo.GetDeployment(ctx, appID, deploymentID)
	if err != nil {
		return "", err
	}
	return deployment.BuildLog, nil
}

func (s *Service) application(ctx context.Context, _ uuid.UUID, appID uuid.UUID, _ bool) (Application, error) {
	if s == nil || s.repo == nil {
		return Application{}, errors.New("deploy: repository is not configured")
	}
	if appID == uuid.Nil {
		return Application{}, fmt.Errorf("%w: invalid application id", ErrValidation)
	}
	app, err := s.repo.GetApplication(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	return app, nil
}

func validateDeployTarget(app Application) error {
	if app.ServerID == uuid.Nil {
		return fmt.Errorf("%w: application has no server assigned", ErrValidation)
	}
	if !ValidSourceType(app.SourceType) {
		return fmt.Errorf("%w: unknown source type %q", ErrValidation, app.SourceType)
	}
	if !SourceTypeImplemented(app.SourceType) {
		return fmt.Errorf("%w: source type %q", ErrSourceNotImplemented, app.SourceType)
	}
	if app.SourceType == SourceDockerfile {
		if err := ValidateDockerfileContent(app.DockerfileContent); err != nil {
			return err
		}
		return ValidateBuildArgs(app.BuildArgs)
	}
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceCompose {
		return validateComposeTarget(app)
	}
	if NormalizeSourceType(app.SourceType, app.Provider) == SourceImage {
		return ValidateImageReference(app.ImageRef)
	}
	if err := validateCloneURL(strings.TrimSpace(app.CloneURL)); err != nil {
		return err
	}
	if err := validateLinkedCloneURL(app); err != nil {
		return err
	}
	if _, err := builds.ParseEngineKind(app.BuildPack); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

func validateComposeTarget(app Application) error {
	if strings.TrimSpace(app.ComposeService) == "" {
		return fmt.Errorf("%w: compose source needs a web service", ErrValidation)
	}
	pasted := strings.TrimSpace(app.ComposeContent) != ""
	repo := strings.TrimSpace(app.ComposeFile) != "" &&
		(strings.TrimSpace(app.Repo) != "" || strings.TrimSpace(app.CloneURL) != "")
	if !pasted && !repo {
		return fmt.Errorf("%w: compose source needs either pasted content or a repository with a compose file",
			ErrValidation)
	}
	return nil
}

func (s *Service) rollbackTarget(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error) {
	if deploymentID != uuid.Nil {
		return s.repo.GetDeployment(ctx, appID, deploymentID)
	}
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		return Deployment{}, err
	}
	running := make([]Deployment, 0, 2)
	for _, dep := range deployments {
		if dep.State == StateRunning {
			running = append(running, dep)
		}
	}
	switch len(running) {
	case 0:
		return Deployment{}, fmt.Errorf("%w: no successful deployment to roll back to", ErrValidation)
	case 1:
		return running[0], nil
	default:
		return running[1], nil
	}
}

func (s *Service) previousContainer(ctx context.Context, appID uuid.UUID) (string, error) {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		s.logger.Warn("deploy: lookup previous container failed", "application_id", appID, "error", err)
		return "", err
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			return dep.ContainerID, nil
		}
	}
	return "", nil
}

func (s *Service) submit(ctx context.Context, app Application, dep Deployment) (Deployment, error) {
	app, created, previous, err := s.createDeployment(ctx, app, dep)
	if err != nil {
		return Deployment{}, err
	}
	if requestreplay.Replayed(ctx) {
		// A replayed deployment was accepted earlier; clear an intentional
		// stop so runtime monitoring does not keep suppressing it.
		s.setDesiredState(ctx, app.ID, "running")
		return created, nil
	}
	s.publishTask(ctx, app, created, taskevents.StatusQueued, "", 0, "")
	if err := s.enqueue(ctx, job{
		app:      app,
		dep:      created,
		previous: previous,
	}); err != nil {
		s.abandon(app, created, err)
		return Deployment{}, err
	}
	// This is the single accepted path for Deploy, DeploySystem and Rollback.
	s.setDesiredState(ctx, app.ID, "running")
	return created, nil
}

func (s *Service) createDeployment(ctx context.Context, app Application, dep Deployment) (Application, Deployment, string, error) {
	unlock := s.locks.lock(app.ID)
	defer unlock()
	fresh, err := s.repo.GetApplication(ctx, app.ID)
	if err != nil {
		return Application{}, Deployment{}, "", err
	}
	app = fresh
	if err := validateDeployTarget(app); err != nil {
		return Application{}, Deployment{}, "", err
	}
	if err := s.checkStoredTarget(ctx, app); err != nil {
		return Application{}, Deployment{}, "", err
	}
	dep.ApplicationID = app.ID
	created, err := s.repo.CreateDeployment(ctx, dep)
	if err != nil {
		return Application{}, Deployment{}, "", err
	}
	if requestreplay.Replayed(ctx) {
		return app, created, "", nil
	}
	previous, err := s.previousContainer(ctx, app.ID)
	if err != nil {
		s.abandon(app, created, fmt.Errorf("deploy: resolve previous container: %w", err))
		return Application{}, Deployment{}, "", fmt.Errorf("deploy: resolve previous container: %w", err)
	}
	return app, created, previous, nil
}

func (s *Service) abandon(app Application, created Deployment, cause error) {
	fresh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created.State = StateFailed
	created.Error = truncateError(cause)
	created.FinishedAt = time.Now().UTC()
	if _, err := s.repo.UpdateDeployment(fresh, created); err != nil {
		s.logger.Error("deploy: could not mark deployment failed",
			"deployment_id", created.ID, "error", err)
	}
	s.publishTask(fresh, app, created, taskevents.StatusFailed, "", 0, created.Error)
}
