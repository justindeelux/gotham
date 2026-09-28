package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	"github.com/justindeelux/gotham/internal/store"
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

// DeployService is the control-plane surface the HTTP layer depends on. It is
// implemented by Service and by fakes in route tests.
type DeployService interface {
	// CreateApplication stores an application together with the environment
	// and storage configuration sent with it.
	CreateApplication(ctx context.Context, userID uuid.UUID, in CreateApplicationInput) (Application, error)
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
	// Source clones the repository; nil selects git on the control plane.
	Source Source
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
// pool. It also owns the application deploy-key lifecycle (registering a key
// on the Git host and removing it with the application). It is safe for
// concurrent use.
type Service struct {
	*Orchestrator
	// registrar talks to the Git host for deploy keys; nil when no provider
	// service is wired (creation then answers a clear error instead of a nil
	// dereference).
	registrar KeyRegistrar
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
	return &Service{Orchestrator: o, registrar: cfg.KeyRegistrar}
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
	app, err := s.application(ctx, userID, appID)
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

// Rollback queues a deployment of a previous release's image. The stored
// image reference is copied onto the new rollback row, which is what makes
// "back to the old version" a normal state-machine run rather than a rewind.
func (s *Service) Rollback(ctx context.Context, userID, appID, deploymentID uuid.UUID) (Deployment, error) {
	if !Enabled() {
		return Deployment{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID)
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
	if _, err := s.application(ctx, userID, appID); err != nil {
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

// application loads an application the caller owns, mapping another user's
// row to ErrNotFound so application IDs cannot be probed.
func (s *Service) application(ctx context.Context, userID, appID uuid.UUID) (Application, error) {
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
	if app.UserID != userID {
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
// When the queue rejects the job the row is marked failed immediately, so it
// never sits in a non-terminal state and blocks the active-deployment index.
func (s *Service) submit(ctx context.Context, app Application, dep Deployment) (Deployment, error) {
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
