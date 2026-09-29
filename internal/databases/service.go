package databases

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

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

// FeatureEnv is the kill switch for the whole databases surface:
// FEATURE_DATABASES=false removes the routes and refuses new databases, so the
// feature can be turned off without a rebuild.
const FeatureEnv = "FEATURE_DATABASES"

// Enabled reports whether the databases feature is on. Only an explicit false
// disables it — unset (or any other value) keeps it enabled, matching
// deploy.Enabled.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// defaultHealthPoll is how often the provisioning wait re-checks the container
// when the engine declares no interval of its own.
const defaultHealthPoll = 500 * time.Millisecond

// namePattern is the API-facing database name: a Docker-safe identifier of
// 1–63 characters. It is also what the derived SQL identifier is built from.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// CreateRequest is the input of Service.Create.
type CreateRequest struct {
	Name    string
	Engine  string
	Version string
	// ServerID is the node that runs the container; it must exist.
	ServerID uuid.UUID
	// PublicPort optionally publishes the engine port on the host. 0 keeps the
	// database internal to the node.
	PublicPort int32
}

// UpdateRequest carries the mutable fields of a database. Only the name can
// change after creation: the public port is a Docker port binding fixed when
// the container is created.
type UpdateRequest struct {
	Name string `json:"name"`
}

// DatabaseService is the control-plane surface the HTTP layer depends on. It
// is implemented by Service and by fakes in the route tests.
type DatabaseService interface {
	// Create provisions a database and returns the row with its credentials.
	Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Database, Credentials, error)
	// List returns the caller's live databases, newest first.
	List(ctx context.Context, userID uuid.UUID) ([]Database, error)
	// Get returns one database the caller owns (404 for anyone else's).
	Get(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	// Credentials returns the decrypted credentials of a database the caller
	// owns.
	Credentials(ctx context.Context, userID, databaseID uuid.UUID) (Credentials, error)
	// Update renames a database the caller owns.
	Update(ctx context.Context, userID, databaseID uuid.UUID, req UpdateRequest) (Database, error)
	// Delete stops and removes the container, then soft-deletes the row; the
	// named volume is kept for the grace window.
	Delete(ctx context.Context, userID, databaseID uuid.UUID) error
	// Start powers the container back on and waits for it to be running.
	Start(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	// Stop shuts the container down; the volume keeps the data.
	Stop(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	// Restart re-runs the container in place.
	Restart(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
}

// Config wires a Service. Store (or an explicit Repository) and Containers are
// required for anything beyond tests; Secret is the key providers.SealSecret
// sealed the credentials with. HealthTimeout and HealthPoll override the
// engine's own window (tests use short values); a zero value keeps the engine
// default and defaultHealthPoll.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Containers creates and drives the database containers through the
	// shared container service; this package never dials an agent itself.
	Containers containers.ContainerService
	// Secret opens the sealed credentials.
	Secret string
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// HealthTimeout overrides every engine's provisioning window.
	HealthTimeout time.Duration
	// HealthPoll is the interval between readiness checks.
	HealthPoll time.Duration
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

// Service is the databases domain service: it validates and provisions
// databases, reads them back for their owner, and drives the container
// lifecycle. It is safe for concurrent use.
type Service struct {
	repo          Repository
	containers    containers.ContainerService
	secret        string
	logger        *slog.Logger
	healthTimeout time.Duration
	healthPoll    time.Duration
}

// Compile-time guarantee that Service satisfies the route-level contract.
var _ DatabaseService = (*Service)(nil)

// NewService builds a Service from cfg. The returned service has no
// repository or container service only when cfg carries none; methods then
// fail with a clear error instead of panicking.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	poll := cfg.HealthPoll
	if poll <= 0 {
		poll = defaultHealthPoll
	}
	return &Service{
		repo:          cfg.repository(),
		containers:    cfg.Containers,
		secret:        cfg.Secret,
		logger:        logger,
		healthTimeout: cfg.HealthTimeout,
		healthPoll:    poll,
	}
}

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil (a nil DatabaseService) when there is no database, no container
// service or the feature flag is off, so callers can pass its result to Mount
// unconditionally.
func NewDefaultService(cfg Config) DatabaseService {
	if cfg.repository() == nil || cfg.Containers == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	return NewService(cfg)
}

// Create provisions a database: it validates the request, generates and seals
// the credentials, writes the row, runs the container through the shared
// container service and waits for the engine's health window before reporting
// the database as running.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Database, Credentials, error) {
	if !Enabled() {
		return Database{}, Credentials{}, ErrDisabled
	}
	if err := s.ready(); err != nil {
		return Database{}, Credentials{}, err
	}
	engine, canonical, err := parseEngine(req.Engine)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	name := strings.TrimSpace(req.Name)
	if !namePattern.MatchString(name) {
		return Database{}, Credentials{}, fmt.Errorf(
			"%w: name must be 1-63 characters of letters, digits, \".\", \"_\" or \"-\"", ErrValidation)
	}
	if err := ValidateVersion(req.Version); err != nil {
		return Database{}, Credentials{}, err
	}
	if req.ServerID == uuid.Nil {
		return Database{}, Credentials{}, fmt.Errorf("%w: server_id is required", ErrValidation)
	}
	if req.PublicPort < 0 || req.PublicPort > 65535 {
		return Database{}, Credentials{}, fmt.Errorf("%w: public_port must be between 0 and 65535", ErrValidation)
	}
	exists, err := s.repo.ServerExists(ctx, req.ServerID)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	if !exists {
		return Database{}, Credentials{}, ErrServerNotFound
	}

	credentials, err := generateCredentials(canonical, name)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	now := time.Now().UTC()
	database := Database{
		ID:         uuid.New(),
		UserID:     userID,
		TeamID:     teams.ScopeFor(ctx, userID).TeamID,
		ServerID:   req.ServerID,
		Name:       name,
		Engine:     canonical,
		Version:    strings.TrimSpace(req.Version),
		Status:     StatusCreating,
		PublicPort: req.PublicPort,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// The ID seeds the named volume, so the volume exists for every row from
	// the moment it is written.
	database.StoragePath = VolumeName(database.ID)

	stored, err := s.repo.CreateDatabase(ctx, database)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	secrets, err := s.storeCredentials(ctx, stored.ID, credentials)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	options, err := buildRunOptions(stored, engine, secrets, s.secret)
	if err != nil {
		return Database{}, Credentials{}, err
	}

	containerID, err := s.containers.Run(ctx, stored.ServerID, options)
	if err != nil {
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, mapContainerError(err)
	}
	stored.ContainerID = containerID
	if stored, err = s.repo.UpdateDatabase(ctx, stored); err != nil {
		// The agent started the container but the row could not record its
		// ID, so the control plane would never be able to manage it again.
		// Best-effort removal beats an orphan container on the node.
		if removeErr := s.containers.Remove(ctx, stored.ServerID, containerID); removeErr != nil &&
			!errors.Is(removeErr, containers.ErrContainerNotFound) &&
			!errors.Is(removeErr, containers.ErrServerNotFound) {
			s.logger.Warn("databases: could not remove container after a failed update",
				"database_id", stored.ID.String(), "container_id", containerID, "error", removeErr)
		}
		return Database{}, Credentials{}, err
	}
	if err := s.waitHealthy(ctx, stored, engine.Healthcheck()); err != nil {
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}
	if stored, err = s.setStatus(ctx, stored, StatusRunning); err != nil {
		return Database{}, Credentials{}, err
	}
	return stored, credentials, nil
}

// List returns the active team's live databases, newest first. Without a team
// context it returns the creator's databases, which is the pre-teams behavior.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Database, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	databases, err := s.repo.ListDatabases(ctx, teams.ScopeFor(ctx, userID))
	if err != nil {
		return nil, err
	}
	if databases == nil {
		return []Database{}, nil
	}
	return databases, nil
}

// Get returns one database of the caller's active team.
func (s *Service) Get(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	return s.database(ctx, userID, databaseID, false)
}

// Credentials opens the sealed credentials of a database the caller may read.
func (s *Service) Credentials(ctx context.Context, userID, databaseID uuid.UUID) (Credentials, error) {
	database, err := s.database(ctx, userID, databaseID, false)
	if err != nil {
		return Credentials{}, err
	}
	secrets, err := s.repo.ListSecrets(ctx, database.ID)
	if err != nil {
		return Credentials{}, err
	}
	return openCredentials(s.secret, secrets)
}

// Update renames a database of the active team. The partial unique index turns
// a collision with a live database into ErrConflict.
func (s *Service) Update(ctx context.Context, userID, databaseID uuid.UUID, req UpdateRequest) (Database, error) {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return Database{}, err
	}
	name := strings.TrimSpace(req.Name)
	if !namePattern.MatchString(name) {
		return Database{}, fmt.Errorf(
			"%w: name must be 1-63 characters of letters, digits, \".\", \"_\" or \"-\"", ErrValidation)
	}
	database.Name = name
	return s.repo.UpdateDatabase(ctx, database)
}

// Delete stops and removes the container of a database of the active team,
// then soft-deletes the row. The named volume is never touched: it is kept for
// the 7-day grace window of the phase rollback note, so the data outlives both
// the container and the row's visibility.
func (s *Service) Delete(ctx context.Context, userID, databaseID uuid.UUID) error {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return err
	}
	if database.ContainerID != "" {
		// A graceful stop first so the engine can flush; it is best-effort
		// because Remove below forces the container down anyway. Failing the
		// delete on a stop error would leave an agent outage unable to delete
		// anything, while Remove still reports a real failure.
		if err := s.containers.Stop(ctx, database.ServerID, database.ContainerID); err != nil {
			s.logger.Warn("databases: stop before delete failed; forcing removal",
				"database_id", database.ID.String(), "error", err)
		}
		if err := s.containers.Remove(ctx, database.ServerID, database.ContainerID); err != nil &&
			!errors.Is(err, containers.ErrContainerNotFound) &&
			!errors.Is(err, containers.ErrServerNotFound) {
			return mapContainerError(err)
		}
	}
	if _, err := s.repo.SoftDeleteDatabase(ctx, database.ID); err != nil {
		return err
	}
	s.logger.Info("databases: deleted; volume retained for the grace window",
		"database_id", database.ID.String(), "volume", database.StoragePath)
	return nil
}

// Start powers the container back on and reports the database running once the
// engine's health window passes.
func (s *Service) Start(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	database, err := s.ownedContainer(ctx, userID, databaseID)
	if err != nil {
		return Database{}, err
	}
	engine, _, err := parseEngine(database.Engine)
	if err != nil {
		return Database{}, err
	}
	if err := s.containers.Start(ctx, database.ServerID, database.ContainerID); err != nil {
		return Database{}, mapContainerError(err)
	}
	if err := s.waitHealthy(ctx, database, engine.Healthcheck()); err != nil {
		s.markError(ctx, database, err)
		return Database{}, err
	}
	return s.setStatus(ctx, database, StatusRunning)
}

// Stop shuts the container down. The volume keeps every byte, so a stopped
// database is fully recoverable by Start.
func (s *Service) Stop(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	database, err := s.ownedContainer(ctx, userID, databaseID)
	if err != nil {
		return Database{}, err
	}
	if err := s.containers.Stop(ctx, database.ServerID, database.ContainerID); err != nil {
		return Database{}, mapContainerError(err)
	}
	return s.setStatus(ctx, database, StatusStopped)
}

// Restart re-runs the container in place and waits for it to be healthy again.
func (s *Service) Restart(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	database, err := s.ownedContainer(ctx, userID, databaseID)
	if err != nil {
		return Database{}, err
	}
	engine, _, err := parseEngine(database.Engine)
	if err != nil {
		return Database{}, err
	}
	if err := s.containers.Restart(ctx, database.ServerID, database.ContainerID); err != nil {
		return Database{}, mapContainerError(err)
	}
	if err := s.waitHealthy(ctx, database, engine.Healthcheck()); err != nil {
		s.markError(ctx, database, err)
		return Database{}, err
	}
	return s.setStatus(ctx, database, StatusRunning)
}

// ready reports a service that was built without its required dependencies.
func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("databases: repository is not configured")
	}
	if s.containers == nil {
		return errors.New("databases: container service is not configured")
	}
	return nil
}

// database loads a database of the caller's active team, mapping a row of
// another team (or, without a team context, of another creator) to ErrNotFound
// so database IDs cannot be probed. A write additionally needs an owner/admin
// role; the read path is open to every member. Soft-deleted rows are already
// filtered out by the repository.
func (s *Service) database(ctx context.Context, userID, databaseID uuid.UUID, write bool) (Database, error) {
	if err := s.ready(); err != nil {
		return Database{}, err
	}
	if databaseID == uuid.Nil {
		return Database{}, fmt.Errorf("%w: invalid database id", ErrValidation)
	}
	database, err := s.repo.GetDatabase(ctx, databaseID)
	if err != nil {
		return Database{}, err
	}
	if err := teams.ScopeFor(ctx, userID).AuthorizeResource(database.TeamID, database.UserID, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return Database{}, err
		}
		return Database{}, ErrNotFound
	}
	return database, nil
}

// ownedContainer loads a database of the active team that has a container to
// act on. Every caller performs a mutation, so it is authorized as a write.
func (s *Service) ownedContainer(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return Database{}, err
	}
	if database.ContainerID == "" {
		return Database{}, fmt.Errorf("%w: database has no container", ErrValidation)
	}
	return database, nil
}

// storeCredentials seals and writes every credential of a database, returning
// the rows it persisted. Those rows are what the run payload opens, so the
// plaintext path is exactly one seal at creation and one open at materialise.
func (s *Service) storeCredentials(ctx context.Context, databaseID uuid.UUID, credentials Credentials) ([]Secret, error) {
	secrets, err := sealCredentials(s.secret, databaseID, credentials)
	if err != nil {
		return nil, err
	}
	for _, secret := range secrets {
		if _, err := s.repo.CreateSecret(ctx, secret); err != nil {
			return nil, err
		}
	}
	return secrets, nil
}

// waitHealthy polls the container until it reports the running state, bounded
// by the engine's window (or Config.HealthTimeout when set).
//
// The list is served by the shared container service, whose List results are
// cached for a short TTL; a fresh observation can therefore lag by up to one
// TTL. The window is generous enough to absorb that, and the agent call is the
// authoritative check.
func (s *Service) waitHealthy(ctx context.Context, database Database, health Healthcheck) error {
	timeout := health.Timeout
	if s.healthTimeout > 0 {
		timeout = s.healthTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		running, err := s.containerRunning(ctx, database)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s did not reach the running state within %s",
				ErrHealthcheck, database.Name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.healthPoll):
		}
	}
}

// containerRunning reports whether the database container is listed as running
// on its node. A container that is not listed yet counts as not running.
func (s *Service) containerRunning(ctx context.Context, database Database) (bool, error) {
	list, err := s.containers.List(ctx, database.ServerID)
	if err != nil {
		return false, mapContainerError(err)
	}
	for _, item := range list {
		if item.ID == database.ContainerID {
			return item.State == "running", nil
		}
	}
	return false, nil
}

// setStatus persists a status transition and returns the updated row.
func (s *Service) setStatus(ctx context.Context, database Database, status Status) (Database, error) {
	database.Status = status
	updated, err := s.repo.UpdateDatabase(ctx, database)
	if err != nil {
		return Database{}, err
	}
	return updated, nil
}

// markError flips a database to the error state after a failed provision or
// lifecycle action. Failures are logged only: the original error is what the
// caller sees, and a second write must not mask it.
func (s *Service) markError(ctx context.Context, database Database, cause error) {
	database.Status = StatusError
	if _, err := s.repo.UpdateDatabase(ctx, database); err != nil {
		s.logger.Error("databases: could not mark the database as errored",
			"database_id", database.ID.String(), "error", err)
		return
	}
	s.logger.Warn("databases: operation failed",
		"database_id", database.ID.String(), "status", StatusError, "error", cause)
}

// mapContainerError translates container-service sentinels onto this package's
// vocabulary; unrecognised errors pass through unchanged.
func mapContainerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, containers.ErrServerNotFound):
		return fmt.Errorf("%w: %v", ErrServerNotFound, err)
	case errors.Is(err, containers.ErrContainerNotFound):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, containers.ErrAgentUnavailable):
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	case errors.Is(err, containers.ErrValidation):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return err
	}
}
