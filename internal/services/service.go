package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
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
	// Logs streams the project's or one compose service's logs.
	Logs(ctx context.Context, userID, serviceID uuid.UUID, composeService string, tail int64, follow bool) (<-chan []byte, error)
	// Deploys returns the deploy history, newest first.
	Deploys(ctx context.Context, userID, serviceID uuid.UUID) ([]Deploy, error)
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
	logger        *slog.Logger
	deployTimeout time.Duration
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
		logger:        logger,
		deployTimeout: timeout,
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
	exists, err := s.repo.ServerExists(ctx, req.ServerID)
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

// List returns the caller's live services, newest first.
func (s *service) List(ctx context.Context, userID uuid.UUID) ([]Service, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	services, err := s.repo.ListServicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if services == nil {
		return []Service{}, nil
	}
	return services, nil
}

// Get returns one service the caller owns.
func (s *service) Get(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	return s.service(ctx, userID, serviceID)
}

// Update patches a service the caller owns and re-validates the result. The
// stored document is always the renderable one: a patch that introduces an
// unresolvable environment reference is rejected.
func (s *service) Update(ctx context.Context, userID, serviceID uuid.UUID, req UpdateRequest) (Service, error) {
	service, err := s.service(ctx, userID, serviceID)
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
	return s.repo.UpdateService(ctx, service)
}

// Delete stops the project and soft-deletes the row. Named volumes are never
// touched: they are the project's data and outlive both the containers and the
// row's visibility.
func (s *service) Delete(ctx context.Context, userID, serviceID uuid.UUID) error {
	service, err := s.service(ctx, userID, serviceID)
	if err != nil {
		return err
	}
	rendered, renderErr := Render(service.ComposeYAML, service.Env)
	if renderErr != nil {
		// The row exists but its document no longer renders (an environment
		// value changed after the last deploy). The delete still proceeds:
		// failing it would make a broken service undeletable. The node keeps
		// the containers until an operator cleans them up.
		s.logger.Warn("services: deleting a service whose document does not render; running project may need manual cleanup",
			"service_id", service.ID.String(), "error", Redact(renderErr.Error(), service.Env))
	} else if err := s.down(ctx, service, rendered); err != nil {
		return err
	}
	if _, err := s.repo.SoftDeleteService(ctx, service.ID); err != nil {
		return err
	}
	s.logger.Info("services: deleted; named volumes retained",
		"service_id", service.ID.String(), "project", ProjectName(service.ID))
	return nil
}

// Deploy renders the stored document, validates it on the node and starts the
// project, recording one deploy row whose snapshot is the rendered document.
func (s *service) Deploy(ctx context.Context, userID, serviceID uuid.UUID) (Service, Deploy, error) {
	if !Enabled() {
		return Service{}, Deploy{}, ErrDisabled
	}
	if err := s.ready(); err != nil {
		return Service{}, Deploy{}, err
	}
	service, err := s.service(ctx, userID, serviceID)
	if err != nil {
		return Service{}, Deploy{}, err
	}
	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, Deploy{}, RedactError(err, service.Env)
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return Service{}, Deploy{}, err
	}
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
	service.Status = StatusRunning
	if service, err = s.repo.UpdateService(ctx, service); err != nil {
		return Service{}, Deploy{}, err
	}
	s.logger.Info("services: deployed", "service_id", service.ID.String(), "project", project)
	return service, deploy, nil
}

// Stop takes the project down. The named volumes keep every byte, so a stopped
// service is fully recoverable by a deploy or a restart.
func (s *service) Stop(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	service, err := s.service(ctx, userID, serviceID)
	if err != nil {
		return Service{}, err
	}
	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	if err := s.down(ctx, service, rendered); err != nil {
		return Service{}, err
	}
	service.Status = StatusStopped
	return s.repo.UpdateService(ctx, service)
}

// Restart restarts a running project in place and starts a stopped one.
func (s *service) Restart(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
	service, err := s.service(ctx, userID, serviceID)
	if err != nil {
		return Service{}, err
	}
	rendered, err := Render(service.ComposeYAML, service.Env)
	if err != nil {
		return Service{}, RedactError(err, service.Env)
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return Service{}, err
	}
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
		return Service{}, err
	}
	service.Status = StatusRunning
	return s.repo.UpdateService(ctx, service)
}

// Containers lists the project's containers as the node reports them.
func (s *service) Containers(ctx context.Context, userID, serviceID uuid.UUID) ([]ComposeContainer, error) {
	service, err := s.service(ctx, userID, serviceID)
	if err != nil {
		return nil, err
	}
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return nil, err
	}
	containers, err := agent.Ps(ctx, ProjectName(service.ID))
	if err != nil {
		return nil, err
	}
	if containers == nil {
		return []ComposeContainer{}, nil
	}
	return containers, nil
}

// Logs streams the project's logs (or one compose service's) from the node.
func (s *service) Logs(ctx context.Context, userID, serviceID uuid.UUID, composeService string, tail int64, follow bool) (<-chan []byte, error) {
	service, err := s.service(ctx, userID, serviceID)
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
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return nil, err
	}
	return agent.Logs(ctx, ProjectName(service.ID), composeService, tail, follow)
}

// Deploys returns the deploy history, newest first.
func (s *service) Deploys(ctx context.Context, userID, serviceID uuid.UUID) ([]Deploy, error) {
	service, err := s.service(ctx, userID, serviceID)
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

// down renders-then-stops helper: it dials the node and runs compose down,
// mapping every failure to a sentinel.
func (s *service) down(ctx context.Context, service Service, rendered RenderedSpec) error {
	agent, err := s.dialAgent(ctx, service.ServerID)
	if err != nil {
		return err
	}
	downCtx, cancel := context.WithTimeout(ctx, s.deployTimeout)
	defer cancel()
	if err := agent.Down(downCtx, ProjectName(service.ID), []byte(rendered.ComposeYAML)); err != nil {
		return err
	}
	return nil
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
		service.Status = status
		if service, updateErr = s.repo.UpdateService(ctx, service); updateErr != nil {
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

// service loads a service the caller owns, mapping another user's row to
// ErrNotFound so service IDs cannot be probed. Soft-deleted rows are already
// filtered out by the repository.
func (s *service) service(ctx context.Context, userID, serviceID uuid.UUID) (Service, error) {
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
	if service.UserID != userID {
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
