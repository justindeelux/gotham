package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// FeatureEnv is the kill switch for the whole applications surface:
// FEATURE_APPLICATIONS=false removes the routes and refuses new deployments,
// leaving Phases 0–3 unaffected.
const FeatureEnv = "FEATURE_APPLICATIONS"

// Enabled reports whether the applications feature is on. Only an explicit
// false disables it — unset (or any other value) keeps it enabled, matching
// ws.RealtimeEnabled.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// DefaultHookTimeout bounds one best-effort Git-host hook call (install on
// create, remove on delete; Config.HookTimeout overrides it).
//
// The budget, with webhooks.hookRollbackTimeout (3s): a create whose provider
// answers at this deadline and whose hook-row write then fails spends at most
// 8s + 3s = 11s before it answers, leaving 4s of the SPA's 15s request timeout
// (web/src/api/http.ts) for everything else. Raising either bound must keep
// the sum safely below that timeout — TestHookBudgetsStayUnderSPARequestTimeout
// in internal/webhooks pins it.
const DefaultHookTimeout = 8 * time.Second

// DeployService is the control-plane surface the HTTP layer depends on. It is
// implemented by Service and by fakes in route tests.
type DeployService interface {
	// CreateApplication stores an application together with the environment
	// and storage configuration sent with it.
	CreateApplication(ctx context.Context, userID uuid.UUID, in CreateApplicationInput) (Application, error)
	// InstallHook installs the provider hook of an application that was just
	// created (BE-4.4). The call is bounded (Config.HookTimeout) and a
	// failure is logged with the configured logger; callers must never fail
	// the create on it — the application row is already committed and the
	// explicit idempotent webhook route is the retry path.
	//
	// attempted is false when no hook lifecycle is wired: there is no outcome
	// to report, and the create response omits the webhook field.
	InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) (attempted bool, err error)
	// ListApplications returns the caller's applications, newest first.
	ListApplications(ctx context.Context, userID uuid.UUID) ([]Application, error)
	// GetApplication returns one application the caller owns (404 otherwise).
	GetApplication(ctx context.Context, userID, appID uuid.UUID) (Application, error)
	// UpdateApplication applies a partial update to the mutable fields.
	UpdateApplication(ctx context.Context, userID, appID uuid.UUID, in UpdateApplicationInput) (Application, error)
	// DeleteApplication stops the current container best effort and deletes
	// the application; its configuration cascades.
	DeleteApplication(ctx context.Context, userID, appID uuid.UUID) error
	// GetEnv returns the environment: plain values and secret references.
	GetEnv(ctx context.Context, userID, appID uuid.UUID) ([]EnvEntry, error)
	// ReplaceEnv replaces the environment collection and returns it as stored.
	ReplaceEnv(ctx context.Context, userID, appID uuid.UUID, entries []EnvEntry) ([]EnvEntry, error)
	// GetStorages returns the application's storage mappings.
	GetStorages(ctx context.Context, userID, appID uuid.UUID) ([]Storage, error)
	// ReplaceStorages replaces the storage collection and returns it as stored.
	ReplaceStorages(ctx context.Context, userID, appID uuid.UUID, storages []Storage) ([]Storage, error)
	// Stop stops the container of the newest deployment.
	Stop(ctx context.Context, userID, appID uuid.UUID) (Deployment, error)
	// Start restarts the container of the newest deployment.
	Start(ctx context.Context, userID, appID uuid.UUID) (Deployment, error)
	// CreateDeployKey generates and registers an SSH deploy key for a private
	// repository; an application that already has one gets it back.
	CreateDeployKey(ctx context.Context, userID, appID uuid.UUID) (DeployKey, error)
	// DeleteDeployKey removes the deploy key from the Git host and the
	// database; an application without one reports false.
	DeleteDeployKey(ctx context.Context, userID, appID uuid.UUID) (bool, error)
	// Deploy queues a deployment of the application's current revision.
	Deploy(ctx context.Context, userID, appID uuid.UUID) (Deployment, error)
	// ListDeployments returns the application's deployments, newest first.
	ListDeployments(ctx context.Context, userID, appID uuid.UUID) ([]Deployment, error)
	// Rollback queues a deployment of a previous release's image. A zero
	// deploymentID selects the previous successful deployment automatically.
	Rollback(ctx context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error)
}

// HookLifecycle installs and removes the Git-host hook that turns a push
// into a deployment. The server wires it to the webhooks service (BE-4.4);
// without it the applications surface keeps working and hooks stay manageable
// through the explicit webhook routes.
//
// InstallHook takes the create request because the public callback origin is
// request-derived (X-Forwarded-Proto + Host), exactly like the explicit
// webhook route; a caller without a request must use that route instead.
type HookLifecycle interface {
	// InstallHook installs the application's provider hook, deriving the
	// public callback origin from r. It is idempotent: an application that
	// already has a hook reports success without installing a second one.
	InstallHook(ctx context.Context, userID, appID uuid.UUID, r *http.Request) error
	// RemoveHook removes the application's provider hook and its stored row;
	// an application without a hook is a success.
	RemoveHook(ctx context.Context, userID, appID uuid.UUID) error
}

// Config wires a Service. Store (or an explicit Repository) is required for
// anything beyond tests; Secret opens sealed application secrets, RedisAddr
// feeds the realtime publisher, and Dial is the mTLS agent dialer. Every
// tunable falls back to a documented default.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Secret is the key providers.SealSecret sealed application secrets with.
	Secret string
	// RedisAddr is the realtime publisher's Redis address; empty disables it.
	RedisAddr string
	// Publisher overrides the Redis publisher built from RedisAddr (tests).
	Publisher Publisher
	// Dial opens a node agent; nil fails deployments with ErrAgentUnavailable.
	Dial DialFunc
	// KeyRegistrar registers and removes deploy keys on the Git host; nil
	// disables deploy-key creation (the routes answer a clear error).
	KeyRegistrar KeyRegistrar
	// Proxy is notified after application mutations and successful
	// deployments so the node's Traefik configuration follows the
	// application state. Best effort: a sync failure is logged, never
	// returned. nil disables the notifications.
	Proxy ProxySync
	// Notifier receives one terminal deployment result (running or failed).
	// Best effort: delivery runs asynchronously and can never fail a
	// deployment. nil disables notifications, which is also the
	// FEATURE_NOTIFICATIONS=false path.
	Notifier Notifier
	// Source clones the repository; nil selects git on the control plane.
	Source Source
	// PreviewCleanup tears down resources that hang off an application
	// outside the deploy schema before the application row is deleted
	// (BE-8.1 preview siblings). A returned error aborts the delete: deleting
	// the base application would cascade the preview bindings away while the
	// siblings survive, leaving them untracked. Container stops inside the
	// callback stay best effort. nil disables the hook.
	PreviewCleanup func(ctx context.Context, appID uuid.UUID) error
	// Hooks resolves the provider-hook lifecycle (BE-4.4). It is a function
	// because the webhook service is built after this one — it consumes this
	// service as its deployer — so the server's closure returns it once it
	// exists. nil, or a closure answering nil, disables automatic hook
	// management; the explicit webhook routes keep working either way.
	Hooks func() HookLifecycle
	// HookTimeout bounds one best-effort hook call against a provider that
	// accepts the connection and then stalls. Zero selects
	// DefaultHookTimeout (8s). The bound is a request-path safety net, not a
	// retry budget: a timeout is logged like any other hook failure.
	HookTimeout time.Duration
	// Emitter overrides the publisher-based realtime emitter (tests).
	Emitter *Emitter
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Workers is the pool size, QueueSize the job buffer.
	Workers     int
	QueueSize   int
	MaxAttempts int
	// StepTimeout bounds clone/push/start; BuildTimeout bounds the build step;
	// HealthTimeout is the post-start health window and HealthPoll its interval.
	StepTimeout   time.Duration
	BuildTimeout  time.Duration
	HealthTimeout time.Duration
	HealthPoll    time.Duration
}

// repository resolves the configured repository implementation.
func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store, c.Secret)
	}
	return nil
}

// Service is the deploy domain service: it validates and queues deployments,
// lists them, drives rollbacks, and runs them on the orchestrator's worker
// pool. It also owns the application hook and deploy-key lifecycles
// (installing/removing the Git-host integration with the application). It is
// safe for concurrent use.
type Service struct {
	*Orchestrator
	// registrar talks to the Git host for deploy keys; nil when no provider
	// service is wired (creation then answers a clear error instead of a nil
	// dereference).
	registrar KeyRegistrar
	// previewCleanup, when set, runs before an application row is deleted (see
	// Config.PreviewCleanup).
	previewCleanup func(ctx context.Context, appID uuid.UUID) error
	// hooks resolves the provider-hook lifecycle lazily (see Config.Hooks).
	hooks func() HookLifecycle
	// hookTimeout bounds one hook call (see Config.HookTimeout).
	hookTimeout time.Duration
}

// Compile-time guarantee that Service satisfies the route-level contract.
var _ DeployService = (*Service)(nil)

// NewService builds a Service from cfg. The returned service has no
// repository only when cfg carries none; methods then fail with a clear
// error instead of panicking. Construction runs the boot-time recovery
// sweep, before any worker can pick up a deployment.
func NewService(cfg Config) *Service {
	o := newOrchestrator(cfg)
	o.recoverStale()
	hookTimeout := cfg.HookTimeout
	if hookTimeout <= 0 {
		hookTimeout = DefaultHookTimeout
	}
	return &Service{
		Orchestrator:   o,
		registrar:      cfg.KeyRegistrar,
		previewCleanup: cfg.PreviewCleanup,
		hooks:          cfg.Hooks,
		hookTimeout:    hookTimeout,
	}
}

// hookLifecycle resolves the configured hook lifecycle, tolerating a closure
// that answers nil (the webhook service is absent).
func (s *Service) hookLifecycle() HookLifecycle {
	if s == nil || s.hooks == nil {
		return nil
	}
	return s.hooks()
}

// recoverStale marks deployments abandoned by a previous control plane
// process as failed. It runs once at construction: a row stuck in
// cloning/building/starting can never resume, and the active-deployment
// partial unique index would turn every later deploy or rollback of that
// application into a permanent 409. The sweep is best-effort — a database
// error is logged and must not stop the control plane from booting.
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

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil (a nil DeployService) when there is no database or the feature
// flag is off, so callers can pass its result to Mount unconditionally.
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

// Close stops the worker pool and releases the realtime publisher.
func (s *Service) Close() error {
	err := s.Orchestrator.Close()
	if closer, ok := s.emitter.pub.(interface{ Close() error }); ok {
		if closeErr := closer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// Deploy queues a deployment of the application's current revision.
func (s *Service) Deploy(ctx context.Context, userID, appID uuid.UUID) (Deployment, error) {
	app, err := s.application(ctx, userID, appID, true)
	if err != nil {
		return Deployment{}, err
	}
	return s.deployApplication(ctx, app)
}

// DeploySystem queues a deployment for a system trigger (a push webhook) that
// has no authenticated caller: the application is addressed by ID because
// ownership was established when its webhook was installed. Everything else —
// validation, the active-deployment guard, the state machine — is exactly what
// Deploy does.
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

// deployApplication validates a loaded application and runs it on the worker
// pool. It is the shared tail of Deploy and DeploySystem.
func (s *Service) deployApplication(ctx context.Context, app Application) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	if err := validateDeployTarget(app); err != nil {
		return Deployment{}, err
	}
	return s.submit(ctx, app, Deployment{Kind: KindDeploy, State: StateQueued})
}

// checkStoredTarget enforces the stored application→node invariant at the queue
// boundary: an application may only run on a node of its own team, or on a
// legacy node without a team. It compares the application's STORED team with
// the node's team — not the caller's active team — so every queue path is
// covered, including rollback and signature-verified system deploys whose
// worker context carries no team scope at all. Caller membership and role stay
// enforced at the entry points (application()). The refusal is ErrNotFound, so
// a foreign node cannot be probed through the deploy surface.
func (s *Service) checkStoredTarget(ctx context.Context, app Application) error {
	if app.ServerID == uuid.Nil {
		// validateDeployTarget reports the missing node with its own message.
		return nil
	}
	teamID, found, err := s.repo.ServerTeam(ctx, app.ServerID)
	if err != nil {
		return err
	}
	if !found {
		return ErrServerNotFound
	}
	if teamID != uuid.Nil && teamID != app.TeamID {
		return ErrServerNotFound
	}
	return nil
}

// Rollback queues a deployment of a previous release's image. The stored
// image reference is copied onto the new rollback row, which is what makes
// "back to the old version" a normal state-machine run rather than a rewind.
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
	if target.State != StateRunning || strings.TrimSpace(target.ImageTag) == "" {
		return Deployment{}, fmt.Errorf("%w: deployment %s has no released image to roll back to",
			ErrValidation, target.ID)
	}
	return s.submit(ctx, app, Deployment{
		Kind:          KindRollback,
		State:         StateQueued,
		ImageTag:      target.ImageTag,
		RegistryImage: target.RegistryImage,
		Digest:        target.Digest,
		RollbackFrom:  target.ID,
	})
}

// ListDeployments returns the application's deployments, newest first.
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

// application loads an application of the caller's active team, mapping a row
// of another team (or, without a team context, of another creator) to
// ErrNotFound so application IDs cannot be probed. A write additionally needs
// an owner/admin role; a read_only member may read.
func (s *Service) application(ctx context.Context, userID, appID uuid.UUID, write bool) (Application, error) {
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
	if err := teams.ScopeFor(ctx, userID).AuthorizeResource(app.TeamID, app.UserID, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return Application{}, err
		}
		return Application{}, ErrNotFound
	}
	return app, nil
}

// validateDeployTarget rejects an application that cannot be deployed: no
// server assigned, no cloneable repository, or an unknown build pack.
func validateDeployTarget(app Application) error {
	if app.ServerID == uuid.Nil {
		return fmt.Errorf("%w: application has no server assigned", ErrValidation)
	}
	if err := validateCloneURL(strings.TrimSpace(app.CloneURL)); err != nil {
		return err
	}
	if _, err := builds.ParseEngineKind(app.BuildPack); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// rollbackTarget resolves the deployment a rollback redeploys: an explicit
// ID when the client named one, otherwise the previous successful release
// (falling back to the only release, which re-runs it).
func (s *Service) rollbackTarget(ctx context.Context, appID, deploymentID uuid.UUID) (Deployment, error) {
	if deploymentID != uuid.Nil {
		return s.repo.GetDeployment(ctx, appID, deploymentID)
	}
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		return Deployment{}, err
	}
	running := make([]Deployment, 0, 2)
	for _, dep := range deployments { // newest first
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

// previousContainer returns the container the next release should retire: the
// newest one this application started, whether its deployment succeeded or
// failed (a container left running by a failed deploy must still be stopped).
func (s *Service) previousContainer(ctx context.Context, appID uuid.UUID) string {
	deployments, err := s.repo.ListDeployments(ctx, appID)
	if err != nil {
		s.logger.Warn("deploy: lookup previous container failed", "application_id", appID, "error", err)
		return ""
	}
	for _, dep := range deployments {
		if dep.ContainerID != "" {
			return dep.ContainerID
		}
	}
	return ""
}

// submit persists a queued deployment, enqueues its run and returns the row.
// It is the single queue boundary of deploy, rollback and system deploys, so
// the stored application→node check runs here: no queue path can hand the
// worker an application bound to another team's node. When the queue rejects
// the job the row is marked failed immediately, so it never sits in a
// non-terminal state and blocks the active-deployment index.
func (s *Service) submit(ctx context.Context, app Application, dep Deployment) (Deployment, error) {
	if err := s.checkStoredTarget(ctx, app); err != nil {
		return Deployment{}, err
	}
	dep.ApplicationID = app.ID
	created, err := s.repo.CreateDeployment(ctx, dep)
	if err != nil {
		return Deployment{}, err
	}
	if err := s.enqueue(ctx, job{
		app:      app,
		dep:      created,
		previous: s.previousContainer(ctx, app.ID),
	}); err != nil {
		s.abandon(created, err)
		return Deployment{}, err
	}
	return created, nil
}

// abandon marks a deployment that never started running as failed.
func (s *Service) abandon(created Deployment, cause error) {
	fresh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created.State = StateFailed
	created.Error = truncateError(cause)
	created.FinishedAt = time.Now().UTC()
	if _, err := s.repo.UpdateDeployment(fresh, created); err != nil {
		s.logger.Error("deploy: could not mark deployment failed",
			"deployment_id", created.ID, "error", err)
	}
}
