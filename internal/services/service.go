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

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

const FeatureEnv = "FEATURE_SERVICES"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const defaultDeployTimeout = 15 * time.Minute

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type CreateRequest struct {
	Name          string
	EnvironmentID uuid.UUID
	ServerID      uuid.UUID
	ComposeYAML   string
	Env           map[string]string
}

type UpdateRequest struct {
	Name          *string           `json:"name,omitempty"`
	ComposeYAML   *string           `json:"compose_yaml,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	EnvironmentID *uuid.UUID        `json:"-"`
	ServerID      *uuid.UUID        `json:"-"`
}

type ServiceFilter struct {
	EnvironmentID uuid.UUID
	ProjectID     uuid.UUID
}

type ServiceService interface {
	Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Service, error)
	List(ctx context.Context, userID uuid.UUID, filter ServiceFilter) ([]Service, error)
	Get(ctx context.Context, userID, serviceID uuid.UUID) (Service, error)
	Update(ctx context.Context, userID, serviceID uuid.UUID, req UpdateRequest) (Service, error)
	Delete(ctx context.Context, userID, serviceID uuid.UUID) error
	Deploy(ctx context.Context, userID, serviceID uuid.UUID) (Service, Deploy, error)
	Stop(ctx context.Context, userID, serviceID uuid.UUID) (Service, error)
	Restart(ctx context.Context, userID, serviceID uuid.UUID) (Service, error)
	Containers(ctx context.Context, userID, serviceID uuid.UUID) ([]ComposeContainer, error)
	Logs(ctx context.Context, userID, serviceID uuid.UUID, composeService string, tail int64, follow bool) (LogStream, error)
	Deploys(ctx context.Context, userID, serviceID uuid.UUID) ([]Deploy, error)
}

type RouteSync interface {
	SyncServer(ctx context.Context, serverID uuid.UUID) error
}

type DialFunc func(ctx context.Context, serverID uuid.UUID) (ComposeAgent, error)

type Config struct {
	GuardDomains  proxy.DomainGuard
	Store         *store.Store
	Repository    Repository
	Dial          DialFunc
	Proxy         RouteSync
	Logger        *slog.Logger
	DeployTimeout time.Duration
}

func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

type service struct {
	guardDomains  proxy.DomainGuard
	repo          Repository
	dial          DialFunc
	proxy         RouteSync
	logger        *slog.Logger
	deployTimeout time.Duration
	locks         keyedLocks
}

var _ ServiceService = (*service)(nil)

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
		guardDomains:  cfg.GuardDomains,
		repo:          cfg.repository(),
		dial:          cfg.Dial,
		proxy:         cfg.Proxy,
		logger:        logger,
		deployTimeout: timeout,
	}
}

type keyedLocks struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

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

func NewDefaultService(cfg Config) ServiceService {
	if cfg.repository() == nil || cfg.Dial == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	return NewService(cfg)
}

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
	teamID := teamIDFor(ctx, userID)
	if req.EnvironmentID == uuid.Nil {
		return Service{}, fmt.Errorf("%w: environment is required", ErrValidation)
	}
	if _, err := s.repo.ResolveEnvironment(ctx, req.EnvironmentID, teamID); err != nil {
		return Service{}, err
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
	releaseDomain, err := s.guardComposeDomains(ctx, req.ComposeYAML, req.Env)
	if err != nil {
		return Service{}, err
	}
	defer releaseDomain()
	exists, err := s.repo.ServerExists(ctx, req.ServerID, teams.ScopeFor(ctx, userID))
	if err != nil {
		return Service{}, err
	}
	if !exists {
		return Service{}, ErrServerNotFound
	}
	now := time.Now().UTC()
	service := Service{
		ID:            uuid.New(),
		UserID:        userID,
		TeamID:        teamID,
		ServerID:      req.ServerID,
		EnvironmentID: req.EnvironmentID,
		Name:          name,
		Status:        StatusCreating,
		ComposeYAML:   req.ComposeYAML,
		Env:           normalizeEnv(req.Env),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return s.repo.CreateService(ctx, service)
}

func (s *service) List(ctx context.Context, userID uuid.UUID, filter ServiceFilter) ([]Service, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if filter.EnvironmentID != uuid.Nil && filter.ProjectID != uuid.Nil {
		return nil, fmt.Errorf("%w: environment_id and project_id are mutually exclusive", ErrValidation)
	}
	if filter.EnvironmentID != uuid.Nil {
		if _, err := s.repo.ResolveEnvironment(ctx, filter.EnvironmentID, teamIDFor(ctx, userID)); err != nil {
			return nil, err
		}
		return s.repo.ListServicesByEnvironment(ctx, filter.EnvironmentID)
	}
	if filter.ProjectID != uuid.Nil {
		if _, err := s.repo.ResolveProject(ctx, filter.ProjectID, teamIDFor(ctx, userID)); err != nil {
			return nil, err
		}
		return s.repo.ListServicesByProject(ctx, filter.ProjectID)
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

func (s *service) Get(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	return s.service(ctx, userID, serviceID, false)
}

func (s *service) Update(ctx context.Context, userID, serviceID uuid.UUID, req UpdateRequest) (Service, error) {
	service, release, err := s.lifecycle(ctx, userID, serviceID)
	if err != nil {
		return Service{}, err
	}
	defer release()
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
	if req.EnvironmentID != nil && *req.EnvironmentID != service.EnvironmentID {
		if _, err := s.repo.ResolveEnvironment(ctx, *req.EnvironmentID, service.TeamID); err != nil {
			return Service{}, err
		}
		collision, err := s.repo.NameInEnvironment(ctx, *req.EnvironmentID, service.Name, service.ID)
		if err != nil {
			return Service{}, err
		}
		if collision {
			return Service{}, fmt.Errorf("%w: a service named %q already exists in the target environment", ErrNameConflict, service.Name)
		}
		service.EnvironmentID = *req.EnvironmentID
	}
	if req.ServerID != nil && *req.ServerID != service.ServerID {
		if *req.ServerID == uuid.Nil {
			return Service{}, fmt.Errorf("%w: server_id is required", ErrValidation)
		}
		exists, err := s.repo.ServerExists(ctx, *req.ServerID, teams.ScopeFor(ctx, userID))
		if err != nil {
			return Service{}, err
		}
		if !exists {
			return Service{}, ErrServerNotFound
		}
		deployed, err := s.repo.HasDeploys(ctx, service.ID)
		if err != nil {
			return Service{}, err
		}
		if deployed {
			return Service{}, ErrServerPinned
		}
		active, err := s.repo.HasActiveDeploy(ctx, service.ID)
		if err != nil {
			return Service{}, err
		}
		if active {
			return Service{}, ErrDeployInFlight
		}
		service.ServerID = *req.ServerID
	}
	if err := validateEnv(service.Env); err != nil {
		return Service{}, err
	}
	if err := Validate(service.ComposeYAML, service.Env); err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	releaseDomain, err := s.guardComposeDomains(ctx, service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, err
	}
	defer releaseDomain()
	if req.EnvironmentID == nil {
		service.EnvironmentID = uuid.Nil
	}
	if req.ServerID == nil {
		service.ServerID = uuid.Nil
	}
	return s.repo.UpdateServiceConfig(ctx, service)
}

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

type redactingLogStream struct {
	inner LogStream
	env   map[string]string
}

var _ LogStream = (*redactingLogStream)(nil)

func (s *redactingLogStream) Chunks() <-chan []byte { return s.inner.Chunks() }

func (s *redactingLogStream) Err() error { return RedactError(s.inner.Err(), s.env) }

func (s *redactingLogStream) Close() error { return s.inner.Close() }

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

func (s *service) setStatus(ctx context.Context, service Service, status Status) (Service, error) {
	updated, err := s.repo.UpdateServiceStatus(ctx, service.ID, status)
	if err != nil {
		return Service{}, err
	}
	return updated, nil
}

func (s *service) lifecycleLock(serviceID uuid.UUID) func() {
	return s.locks.acquire(serviceID)
}

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

func (s *service) syncProxy(ctx context.Context, serverID uuid.UUID) {
	if s.proxy == nil || serverID == uuid.Nil {
		return
	}
	if err := s.proxy.SyncServer(ctx, serverID); err != nil {
		s.logger.Warn("services: proxy resync after a lifecycle change failed",
			"server_id", serverID.String(), "error", err)
	}
}

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

func (s *service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("services: repository is not configured")
	}
	return nil
}

func teamIDFor(ctx context.Context, userID uuid.UUID) uuid.UUID {
	scope := teams.ScopeFor(ctx, userID)
	if scope.Active() {
		return scope.TeamID
	}
	return teams.PersonalTeamID(userID)
}

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

func normalizeEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	return env
}
