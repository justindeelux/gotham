package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// FeatureEnv is the kill switch for the whole services surface:
// FEATURE_SERVICES=false removes the routes and refuses new services, so the
// feature can be turned off without a rebuild.
const FeatureEnv = "FEATURE_SERVICES"

// Enabled reports whether the services feature is on. Only an explicit false
// disables it — unset (or any other value) keeps it enabled, matching
// databases.Enabled and deploy.Enabled.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// defaultDeployTimeout bounds one deploy's agent calls when Config carries no
// timeout. An up can pull images, so the default is generous.
const defaultDeployTimeout = 15 * time.Minute

// namePattern is the API-facing service name: a Docker-safe identifier of
// 1–63 characters.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// envKeyPattern is the environment variable name alphabet.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CreateRequest is the input of Service.Create. The document is validated
// (rendered against Env) before it is stored, so a stored service is always
// deployable.
type CreateRequest struct {
	Name string
	// ServerID is the node that runs the project; it must exist.
	ServerID uuid.UUID
	// ComposeYAML is the user-supplied compose document.
	ComposeYAML string
	// Env is the ${VAR} substitution input.
	Env map[string]string
}

// UpdateRequest carries the mutable fields of a service. A nil pointer keeps
// the stored value; an empty Env map clears the environment.
type UpdateRequest struct {
	Name        *string           `json:"name,omitempty"`
	ComposeYAML *string           `json:"compose_yaml,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// ServiceService is the control-plane surface the HTTP layer depends on. It is
// implemented by Service and by fakes in the route tests.
type ServiceService interface {
	// Create stores a validated service (status creating); it does not
	// deploy.
	Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Service, error)
	// List returns the caller's live services, newest first.
	List(ctx context.Context, userID uuid.UUID) ([]Service, error)
	// Get returns one service the caller owns (404 for anyone else's).
	Get(ctx context.Context, userID, serviceID uuid.UUID) (Service, error)
	// Update patches the mutable fields and re-validates the result.
	Update(ctx context.Context, userID, serviceID uuid.UUID, req UpdateRequest) (Service, error)
	// Delete stops the project (named volumes intact) and soft-deletes the
	// row.
	Delete(ctx context.Context, userID, serviceID uuid.UUID) error
	// Deploy renders, validates and starts the project, recording one deploy
	// row.
	Deploy(ctx context.Context, userID, serviceID uuid.UUID) (Service, Deploy, error)
	// Stop takes the project down; named volumes keep their data.
	Stop(ctx context.Context, userID, serviceID uuid.UUID) (Service, error)
	// Restart restarts a running project in place, or starts a stopped one.
	Restart(ctx context.Context, userID, serviceID uuid.UUID) (Service, error)
	// Containers lists the project's containers from the node.
	Containers(ctx context.Context, userID, serviceID uuid.UUID) ([]ComposeContainer, error)
	// Logs streams the project's or one compose service's logs. The returned
	// stream owns the node connection and closes it when it ends.
	Logs(ctx context.Context, userID, serviceID uuid.UUID, composeService string, tail int64, follow bool) (LogStream, error)
	// Deploys returns the deploy history, newest first.
	Deploys(ctx context.Context, userID, serviceID uuid.UUID) ([]Deploy, error)
}

// RouteSync re-synchronizes one node's Phase 6 proxy configuration. It is
// satisfied by proxy.ProxyService and called best effort after a lifecycle
// change that affects what a domain routes to (or whether it routes at all).
type RouteSync interface {
	SyncServer(ctx context.Context, serverID uuid.UUID) error
}

// DialFunc opens the node agent's ComposeService for one server.
type DialFunc func(ctx context.Context, serverID uuid.UUID) (ComposeAgent, error)

// Config wires a Service. Store (or an explicit Repository) and Dial are
// required for anything beyond tests.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is
	// set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Dial opens the compose service on the target node; it is plugged in by
	// the HTTP wiring with the servers package's mTLS dialer.
	Dial DialFunc
	// Proxy receives a best-effort resync after a deploy, stop, restart or
	// delete so the node's Traefik configuration follows the service's
	// routing state. nil disables the notifications.
	Proxy RouteSync
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// DeployTimeout overrides how long one deploy's agent calls may take.
	DeployTimeout time.Duration
}

// repository resolves the configured repository implementation.
func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

// service is the compose-service domain service: it validates and stores
// documents, renders them per deploy and drives the node agent's compose
// surface. It is safe for concurrent use.
type service struct {
	repo          Repository
	dial          DialFunc
	proxy         RouteSync
	logger        *slog.Logger
	deployTimeout time.Duration
	locks         keyedLocks
}

// Compile-time guarantee that service satisfies the route-level contract.
var _ ServiceService = (*service)(nil)

// NewService builds the compose-service domain service from cfg. The returned
// service has no repository or dialer only when cfg carries none; methods then
// fail with a clear error instead of panicking.
func NewService(cfg Config) ServiceService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.DeployTimeout
	if timeout <= 0 {
		timeout = defaultDeployTimeout
	}
	return &service{
		repo:          cfg.repository(),
		dial:          cfg.Dial,
		proxy:         cfg.Proxy,
		logger:        logger,
		deployTimeout: timeout,
	}
}

// keyedLocks serializes work per key (one service id) with a refcounted entry
// per key, so the map stays bounded by the operations in flight. It is the
// in-process lock the single control plane process uses for lifecycle
// serialization (the proxy service relies on the same single-process
// assumption).
type keyedLocks struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*keyedLock
}

// keyedLock is one key's mutex plus its holder/waiter count.
type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// acquire takes the key's lock and returns its release function.
func (l *keyedLocks) acquire(key uuid.UUID) func() {
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[uuid.UUID]*keyedLock)
	}
	entry, ok := l.entries[key]
	if !ok {
		entry = &keyedLock{}
		l.entries[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.entries, key)
		}
		l.mu.Unlock()
		entry.mu.Unlock()
	}
}

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil (a nil ServiceService) when there is no database, no agent
// dialer or the feature flag is off, so callers can pass its result to Mount
// unconditionally.
func NewDefaultService(cfg Config) ServiceService {
	if cfg.repository() == nil || cfg.Dial == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	return NewService(cfg)
}

// Create stores a validated service. The document is rendered against the
// request's environment, so a service that would fail to render is rejected
// here instead of at deploy time.
func (s *service) Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Service, error) {
	if !Enabled() {
		return Service{}, ErrDisabled
	}
	if err := s.ready(); err != nil {
		return Service{}, err
	}
	name := strings.TrimSpace(req.Name)
	if !namePattern.MatchString(name) {
		return Service{}, fmt.Errorf(
			"%w: name must be 1-63 characters of letters, digits, \".\", \"_\" or \"-\"", ErrValidation)
	}
	if req.ServerID == uuid.Nil {
		return Service{}, fmt.Errorf("%w: server_id is required", ErrValidation)
	}
	if err := validateEnv(req.Env); err != nil {
		return Service{}, err
	}
	if err := Validate(req.ComposeYAML, req.Env); err != nil {
		return Service{}, RedactError(err, req.Env)
	}
	exists, err := s.repo.ServerExists(ctx, req.ServerID, teams.ScopeFor(ctx, userID))
	if err != nil {
		return Service{}, err
	}
	if !exists {
		return Service{}, ErrServerNotFound
	}
	now := time.Now().UTC()
	service := Service{
		ID:          uuid.New(),
		UserID:      userID,
		TeamID:      teams.ScopeFor(ctx, userID).TeamID,
		ServerID:    req.ServerID,
		Name:        name,
		Status:      StatusCreating,
		ComposeYAML: req.ComposeYAML,
		Env:         normalizeEnv(req.Env),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return s.repo.CreateService(ctx, service)
}

// List returns the active team's live services, newest first. Without a team
// context it returns the creator's services, which is the pre-teams behavior.
func (s *service) List(ctx context.Context, userID uuid.UUID) ([]Service, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	services, err := s.repo.ListServices(ctx, teams.ScopeFor(ctx, userID))
	if err != nil {
		return nil, err
	}
	if services == nil {
		return []Service{}, nil
	}
	return services, nil
}

// Get returns one service of the caller's active team.
func (s *service) Get(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	return s.service(ctx, userID, serviceID, false)
}

// Update patches a service of the active team and re-validates the result. The
// stored document is always the renderable one: a patch that introduces an
// unresolvable environment reference is rejected. The write touches only the
// config columns, so it can never clobber a lifecycle status a concurrent
// deploy/stop is writing.
func (s *service) Update(ctx context.Context, userID, serviceID uuid.UUID, req UpdateRequest) (Service, error) {
	service, err := s.service(ctx, userID, serviceID, true)
	if err != nil {
		return Service{}, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !namePattern.MatchString(name) {
			return Service{}, fmt.Errorf(
				"%w: name must be 1-63 characters of letters, digits, \".\", \"_\" or \"-\"", ErrValidation)
		}
		service.Name = name
	}
	if req.ComposeYAML != nil {
		service.ComposeYAML = *req.ComposeYAML
	}
	if req.Env != nil {
		service.Env = normalizeEnv(req.Env)
	}
	if err := validateEnv(service.Env); err != nil {
		return Service{}, err
	}
	if err := Validate(service.ComposeYAML, service.Env); err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	return s.repo.UpdateServiceConfig(ctx, service)
}

// Delete stops the project and soft-deletes the row. Named volumes are never
// touched: they are the project's data and outlive both the containers and the
// row's visibility.
//
// When the stored document no longer renders, the newest successfully
// deployed snapshot is used to stop the project instead of skipping the node
// call: soft-deleting a row must never hide a project that may still be
// running. If no snapshot exists either, the row is retained with an error so
// an operator can fix or redeploy the service.
func (s *service) Delete(ctx context.Context, userID, serviceID uuid.UUID) error {
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return err
	}
	defer release()

	rendered, renderErr := Render(service.ComposeYAML, service.Env)
	if renderErr == nil {
		if err := s.down(ctx, service, []byte(rendered.ComposeYAML)); err != nil {
			return err
		}
	} else {
		snapshot, snapshotErr := s.latestRunningSnapshot(ctx, service)
		if snapshotErr != nil {
			return snapshotErr
		}
		if snapshot == nil {
			return fmt.Errorf(
				"%w: the stored document no longer renders (%s) and there is no successful deploy snapshot to stop it; fix the document or redeploy first",
				ErrValidation, Redact(renderErr.Error(), service.Env))
		}
		s.logger.Warn("services: stopping from the newest deployed snapshot because the stored document no longer renders",
			"service_id", service.ID.String(), "deploy_id", snapshot.ID.String())
		if err := s.down(ctx, service, []byte(snapshot.ComposeYAML)); err != nil {
			return err
		}
	}
	if _, err := s.repo.SoftDeleteService(ctx, service.ID); err != nil {
		return err
	}
	s.logger.Info("services: deleted; named volumes retained",
		"service_id", service.ID.String(), "project", ProjectName(service.ID))
	s.syncProxy(ctx, service.ServerID)
	return nil
}

// latestRunningSnapshot returns the newest successfully deployed rendered
// document, or nil when the service was never deployed successfully.
func (s *service) latestRunningSnapshot(ctx context.Context, service Service) (*Deploy, error) {
	deploys, err := s.repo.ListServiceDeploys(ctx, service.ID, deployHistoryLimit)
	if err != nil {
		return nil, err
	}
	for i := range deploys {
		if deploys[i].State == DeployRunning && deploys[i].ComposeYAML != "" {
			return &deploys[i], nil
		}
	}
	return nil, nil
}

// Deploy renders the stored document, validates it on the node and starts the
// project, recording one deploy row whose snapshot is the rendered document.
// It is serialized with the other lifecycle operations of the same service and
// writes only the status column on completion, so a concurrent configuration
// edit is never overwritten.
func (s *service) Deploy(ctx context.Context, userID, serviceID uuid.UUID) (Service, Deploy, error) {
	if !Enabled() {
		return Service{}, Deploy{}, ErrDisabled
	}
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return Service{}, Deploy{}, err
	}
	defer release()

	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, Deploy{}, RedactError(err, service.Env)
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return Service{}, Deploy{}, err
	}
	defer func() { _ = agent.Close() }()
	project := ProjectName(service.ID)
	now := time.Now().UTC()
	deploy, err := s.repo.CreateServiceDeploy(ctx, Deploy{
		ID:          uuid.New(),
		ServiceID:   service.ID,
		State:       DeployDeploying,
		ComposeYAML: rendered.ComposeYAML,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return Service{}, Deploy{}, err
	}

	deployCtx, cancel := context.WithTimeout(ctx, s.deployTimeout)
	defer cancel()
	composeYAML := []byte(rendered.ComposeYAML)
	if _, err := agent.Validate(deployCtx, project, composeYAML); err != nil {
		// Nothing on the node changed: the previous version, if any, is
		// still the running one, so the service keeps its status.
		return s.failDeploy(ctx, service, deploy, "", err)
	}
	if err := agent.Up(deployCtx, project, composeYAML, false); err != nil {
		return s.failDeploy(ctx, service, deploy, StatusError, err)
	}
	deploy.State = DeployRunning
	deploy.FinishedAt = time.Now().UTC()
	if deploy, err = s.repo.UpdateServiceDeploy(ctx, deploy); err != nil {
		return Service{}, Deploy{}, err
	}
	if service, err = s.setStatus(ctx, service, StatusRunning); err != nil {
		return Service{}, Deploy{}, err
	}
	s.logger.Info("services: deployed", "service_id", service.ID.String(), "project", project)
	s.syncProxy(ctx, service.ServerID)
	return service, deploy, nil
}

// Stop takes the project down. The named volumes keep every byte, so a stopped
// service is fully recoverable by a deploy or a restart.
func (s *service) Stop(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return Service{}, err
	}
	defer release()

	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	if err := s.down(ctx, service, []byte(rendered.ComposeYAML)); err != nil {
		return Service{}, err
	}
	service, err = s.setStatus(ctx, service, StatusStopped)
	if err != nil {
		return Service{}, err
	}
	s.syncProxy(ctx, service.ServerID)
	return service, nil
}

// Restart restarts a running project in place and starts a stopped one.
func (s *service) Restart(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return Service{}, err
	}
	defer release()

	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return Service{}, err
	}
	defer func() { _ = agent.Close() }()
	deployCtx, cancel := context.WithTimeout(ctx, s.deployTimeout)
	defer cancel()
	project := ProjectName(service.ID)
	composeYAML := []byte(rendered.ComposeYAML)
	if service.Status == StatusRunning {
		err = agent.Up(deployCtx, project, composeYAML, true)
	} else {
		err = agent.Up(deployCtx, project, composeYAML, false)
	}
	if err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	service, err = s.setStatus(ctx, service, StatusRunning)
	if err != nil {
		return Service{}, err
	}
	s.syncProxy(ctx, service.ServerID)
	return service, nil
}

// Containers lists the project's containers as the node reports them.
func (s *service) Containers(ctx context.Context, userID, serviceID uuid.UUID) ([]ComposeContainer, error) {
	service, err := s.service(ctx, userID, serviceID, false)
	if err != nil {
		return nil, err
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = agent.Close() }()
	containers, err := agent.Ps(ctx, ProjectName(service.ID))
	if err != nil {
		return nil, RedactError(err, service.Env)
	}
	if containers == nil {
		return []ComposeContainer{}, nil
	}
	return containers, nil
}

// Logs streams the project's logs (or one compose service's) from the node.
// The returned stream owns the agent connection and closes it when it ends.
//
// The compose service selector is validated against the stored document before
// the node is dialed, so an unknown selector is a validation error instead of
// a CLI refusal that would arrive as the stream's first output. The terminal
// stream error is redacted like every other node error: only application log
// content is passed through untouched.
func (s *service) Logs(ctx context.Context, userID, serviceID uuid.UUID, composeService string, tail int64, follow bool) (LogStream, error) {
	service, err := s.service(ctx, userID, serviceID, false)
	if err != nil {
		return nil, err
	}
	composeService = strings.TrimSpace(composeService)
	if composeService != "" && !composeServiceNamePattern.MatchString(composeService) {
		return nil, fmt.Errorf("%w: invalid compose service name", ErrValidation)
	}
	if tail < 0 {
		return nil, fmt.Errorf("%w: tail must not be negative", ErrValidation)
	}
	if composeService != "" {
		// A document that no longer renders is not fatal for a log read (the
		// node may still have the project's file); the selector check simply
		// cannot run and the agent validates instead.
		if rendered, renderErr := Render(service.ComposeYAML, service.Env); renderErr == nil {
			if !slices.Contains(rendered.Spec.Services, composeService) {
				return nil, fmt.Errorf("%w: no compose service %q in this project", ErrValidation, composeService)
			}
		}
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return nil, err
	}
	stream, err := agent.Logs(ctx, ProjectName(service.ID), composeService, tail, follow)
	if err != nil {
		_ = agent.Close()
		return nil, RedactError(err, service.Env)
	}
	return &redactingLogStream{inner: stream, env: service.Env}, nil
}

// redactingLogStream applies the service environment's redaction boundary to a
// node log stream's terminal error, which is stored and logged after the HTTP
// response has already started. Chunks (actual application log content) pass
// through untouched, and the error chain stays matchable through Unwrap.
type redactingLogStream struct {
	inner LogStream
	env   map[string]string
}

// Compile-time guarantee.
var _ LogStream = (*redactingLogStream)(nil)

// Chunks implements LogStream.
func (s *redactingLogStream) Chunks() <-chan []byte { return s.inner.Chunks() }

// Err implements LogStream.
func (s *redactingLogStream) Err() error { return RedactError(s.inner.Err(), s.env) }

// Close implements LogStream.
func (s *redactingLogStream) Close() error { return s.inner.Close() }

// Deploys returns the deploy history, newest first.
func (s *service) Deploys(ctx context.Context, userID, serviceID uuid.UUID) ([]Deploy, error) {
	service, err := s.service(ctx, userID, serviceID, false)
	if err != nil {
		return nil, err
	}
	deploys, err := s.repo.ListServiceDeploys(ctx, service.ID, deployHistoryLimit)
	if err != nil {
		return nil, err
	}
	if deploys == nil {
		return []Deploy{}, nil
	}
	return deploys, nil
}

// down dials the node and runs compose down with the given rendered document,
// mapping every failure to a sentinel and redacting the service environment
// values from the message.
func (s *service) down(ctx context.Context, service Service, composeYAML []byte) error {
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return err
	}
	defer func() { _ = agent.Close() }()
	downCtx, cancel := context.WithTimeout(ctx, s.deployTimeout)
	defer cancel()
	if err := agent.Down(downCtx, ProjectName(service.ID), composeYAML); err != nil {
		return RedactError(err, service.Env)
	}
	return nil
}

// setStatus writes only the status column, so a lifecycle completion can never
// overwrite a concurrent configuration edit with a stale row.
func (s *service) setStatus(ctx context.Context, service Service, status Status) (Service, error) {
	updated, err := s.repo.UpdateServiceStatus(ctx, service.ID, status)
	if err != nil {
		return Service{}, err
	}
	return updated, nil
}

// lifecycleLock serializes the lifecycle operations (deploy, stop, restart,
// delete) of one service so two actions cannot interleave and report
// contradictory outcomes. A single control plane process makes an in-process
// lock sufficient, matching the proxy service's single-process assumption.
func (s *service) lifecycleLock(serviceID uuid.UUID) func() {
	return s.locks.acquire(serviceID)
}

// lifecycle loads a service of the caller's active team while holding its
// lifecycle lock. Every lifecycle operation mutates, so it is authorized as a
// write. The authoritative row read happens after the lock is acquired, so a
// queued operation always acts on the state its predecessor left behind: a
// Restart queued behind a Delete reads the deleted row and refuses instead of
// resurrecting containers. The returned release function must be deferred by
// the caller.
func (s *service) lifecycle(ctx context.Context, userID, serviceID uuid.UUID) (Service, func(), error) {
	if err := s.ready(); err != nil {
		return Service{}, nil, err
	}
	if serviceID == uuid.Nil {
		return Service{}, nil, fmt.Errorf("%w: invalid service id", ErrValidation)
	}
	release := s.lifecycleLock(serviceID)
	service, err := s.service(ctx, userID, serviceID, true)
	if err != nil {
		release()
		return Service{}, nil, err
	}
	return service, release, nil
}

// syncProxy asks the Phase 6 proxy to re-synchronize the node's routing
// configuration after a lifecycle change. It is best effort by contract: the
// mutation is already durable, a failed sync is logged and the next sync
// converges the node.
func (s *service) syncProxy(ctx context.Context, serverID uuid.UUID) {
	if s.proxy == nil || serverID == uuid.Nil {
		return
	}
	if err := s.proxy.SyncServer(ctx, serverID); err != nil {
		s.logger.Warn("services: proxy resync after a lifecycle change failed",
			"server_id", serverID.String(), "error", err)
	}
}

// failDeploy records a failed attempt (error redacted) and returns the
// original error. status optionally moves the service itself; an empty status
// keeps it, which is what a validation failure before any node change gets.
func (s *service) failDeploy(ctx context.Context, service Service, deploy Deploy, status Status, cause error) (Service, Deploy, error) {
	err := RedactError(cause, service.Env)
	deploy.State = DeployFailed
	deploy.Error = err.Error()
	deploy.FinishedAt = time.Now().UTC()
	updated, updateErr := s.repo.UpdateServiceDeploy(ctx, deploy)
	if updateErr != nil {
		s.logger.Error("services: could not record a failed deploy",
			"service_id", service.ID.String(), "error", updateErr)
		return Service{}, Deploy{}, err
	}
	if status != "" {
		if service, updateErr = s.setStatus(ctx, service, status); updateErr != nil {
			return Service{}, Deploy{}, err
		}
	}
	return service, updated, err
}

// ready reports a service that was built without its required dependencies.
func (s *service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("services: repository is not configured")
	}
	return nil
}

// service loads a service of the caller's active team, mapping a row of
// another team (or, without a team context, of another creator) to ErrNotFound
// so service IDs cannot be probed. A write additionally needs an owner/admin
// role. Soft-deleted rows are already filtered out by the repository.
func (s *service) service(ctx context.Context, userID, serviceID uuid.UUID, write bool) (Service, error) {
	if err := s.ready(); err != nil {
		return Service{}, err
	}
	if serviceID == uuid.Nil {
		return Service{}, fmt.Errorf("%w: invalid service id", ErrValidation)
	}
	service, err := s.repo.GetService(ctx, serviceID)
	if err != nil {
		return Service{}, err
	}
	if err := teams.ScopeFor(ctx, userID).AuthorizeResource(service.TeamID, service.UserID, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return Service{}, err
		}
		return Service{}, ErrNotFound
	}
	return service, nil
}

// dialAgent opens the compose service on the service's node.
func (s *service) dialAgent(ctx context.Context, serverID uuid.UUID) (ComposeAgent, error) {
	if s.dial == nil {
		return nil, fmt.Errorf("%w: no agent dialer is configured", ErrAgentUnavailable)
	}
	agent, err := s.dial(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	return agent, nil
}

// validateEnv checks environment variable names (values are never inspected).
func validateEnv(env map[string]string) error {
	if len(env) > MaxEnvVars {
		return fmt.Errorf("%w: at most %d environment variables", ErrValidation, MaxEnvVars)
	}
	for key := range env {
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("%w: invalid environment variable name %q", ErrValidation, key)
		}
	}
	return nil
}

// normalizeEnv makes a nil map an empty one, so jsonb writes are stable.
func normalizeEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	return env
}
