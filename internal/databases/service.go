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

const FeatureEnv = "FEATURE_DATABASES"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const defaultHealthPoll = 500 * time.Millisecond

const cleanupTimeout = 30 * time.Second

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

type CreateRequest struct {
	Name          string
	Engine        string
	Version       string
	EnvironmentID uuid.UUID
	ServerID      uuid.UUID
	PublicPort    int32
}

type UpdateRequest struct {
	Name          *string    `json:"name,omitempty"`
	EnvironmentID *uuid.UUID `json:"-"`
	ServerID      *uuid.UUID `json:"-"`
}

type DatabaseFilter struct {
	EnvironmentID uuid.UUID
	ProjectID     uuid.UUID
}

type DatabaseService interface {
	Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Database, Credentials, error)
	List(ctx context.Context, userID uuid.UUID, filter DatabaseFilter) ([]Database, error)
	Get(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	Credentials(ctx context.Context, userID, databaseID uuid.UUID) (Credentials, error)
	Update(ctx context.Context, userID, databaseID uuid.UUID, req UpdateRequest) (Database, error)
	Delete(ctx context.Context, userID, databaseID uuid.UUID) error
	Start(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	Stop(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	Restart(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
}

type Config struct {
	Store         *store.Store
	Repository    Repository
	Containers    containers.ContainerService
	Secret        string
	Logger        *slog.Logger
	HealthTimeout time.Duration
	HealthPoll    time.Duration
	Leases        *JobLeases
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

type Service struct {
	repo          Repository
	containers    containers.ContainerService
	secret        string
	logger        *slog.Logger
	healthTimeout time.Duration
	healthPoll    time.Duration
	leases        *JobLeases
}

var _ DatabaseService = (*Service)(nil)

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
		leases:        cfg.Leases,
	}
}

func NewDefaultService(cfg Config) DatabaseService {
	if cfg.repository() == nil || cfg.Containers == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	return NewService(cfg)
}

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
	teamID := teamIDFor(ctx, userID)
	if req.EnvironmentID == uuid.Nil {
		return Database{}, Credentials{}, fmt.Errorf("%w: environment is required", ErrValidation)
	}
	if _, err := s.repo.ResolveEnvironment(ctx, req.EnvironmentID, teamID); err != nil {
		return Database{}, Credentials{}, err
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
	exists, err := s.repo.ServerExists(ctx, req.ServerID, teams.ScopeFor(ctx, userID))
	if err != nil {
		return Database{}, Credentials{}, err
	}
	if !exists {
		return Database{}, Credentials{}, ErrServerNotFound
	}
	if req.PublicPort > 0 {
		inUse, err := s.repo.PublicPortInUse(ctx, req.ServerID, req.PublicPort)
		if err != nil {
			return Database{}, Credentials{}, err
		}
		if inUse {
			return Database{}, Credentials{}, fmt.Errorf(
				"%w: public port %d is already in use on this node", ErrPortConflict, req.PublicPort)
		}
	}

	credentials, err := generateCredentials(canonical, name)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	now := time.Now().UTC()
	database := Database{
		ID:            uuid.New(),
		UserID:        userID,
		TeamID:        teamID,
		ServerID:      req.ServerID,
		EnvironmentID: req.EnvironmentID,
		Name:          name,
		Engine:        canonical,
		Version:       strings.TrimSpace(req.Version),
		Status:        StatusCreating,
		PublicPort:    req.PublicPort,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	database.StoragePath = VolumeName(database.ID)

	stored, err := s.repo.CreateDatabase(ctx, database)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	if err := s.ensureImage(ctx, stored, engine.Image(stored.Version)); err != nil {
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}
	secrets, err := s.storeCredentials(ctx, stored.ID, credentials)
	if err != nil {
		s.rollbackCredentials(ctx, stored)
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}
	options, err := buildRunOptions(stored, engine, secrets, s.secret)
	if err != nil {
		s.rollbackCredentials(ctx, stored)
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}

	containerID, err := s.containers.Run(ctx, stored.ServerID, options)
	if err != nil {
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, mapContainerError(err)
	}
	updated, err := s.repo.UpdateDatabaseContainer(ctx, stored.ID, containerID)
	if err != nil {
		s.markError(ctx, stored, err)
		s.removeContainer(ctx, stored.ServerID, containerID)
		return Database{}, Credentials{}, err
	}
	stored = updated
	if err := s.waitHealthy(ctx, stored, engine.Healthcheck()); err != nil {
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}
	updated, err = s.repo.UpdateDatabaseStatus(ctx, stored.ID, StatusRunning)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.removeContainer(ctx, stored.ServerID, stored.ContainerID)
			return Database{}, Credentials{}, err
		}
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}
	return updated, credentials, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, filter DatabaseFilter) ([]Database, error) {
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
		return s.repo.ListDatabasesByEnvironment(ctx, filter.EnvironmentID)
	}
	if filter.ProjectID != uuid.Nil {
		if _, err := s.repo.ResolveProject(ctx, filter.ProjectID, teamIDFor(ctx, userID)); err != nil {
			return nil, err
		}
		return s.repo.ListDatabasesByProject(ctx, filter.ProjectID)
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

func (s *Service) Get(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	return s.database(ctx, userID, databaseID, false)
}

func (s *Service) Credentials(ctx context.Context, userID, databaseID uuid.UUID) (Credentials, error) {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return Credentials{}, err
	}
	secrets, err := s.repo.ListSecrets(ctx, database.ID)
	if err != nil {
		return Credentials{}, err
	}
	return openCredentials(s.secret, secrets)
}

func (s *Service) Update(ctx context.Context, userID, databaseID uuid.UUID, req UpdateRequest) (Database, error) {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return Database{}, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !namePattern.MatchString(name) {
			return Database{}, fmt.Errorf(
				"%w: name must be 1-63 characters of letters, digits, \".\", \"_\" or \"-\"", ErrValidation)
		}
	}
	if req.ServerID != nil && *req.ServerID != database.ServerID {
		if held := s.leases.Held(databaseID); held == JobLeaseBackup || held == JobLeaseRestore {
			return Database{}, ErrDeployInFlight
		}
	}
	if err := s.claimUpdate(databaseID); err != nil {
		return Database{}, err
	}
	defer s.leases.Release(databaseID)
	database, err = s.database(ctx, userID, databaseID, true)
	if err != nil {
		return Database{}, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !namePattern.MatchString(name) {
			return Database{}, fmt.Errorf(
				"%w: name must be 1-63 characters of letters, digits, \".\", \"_\" or \"-\"", ErrValidation)
		}
		database.Name = name
	}
	if req.EnvironmentID != nil && *req.EnvironmentID != database.EnvironmentID {
		if _, err := s.repo.ResolveEnvironment(ctx, *req.EnvironmentID, database.TeamID); err != nil {
			return Database{}, err
		}
		collision, err := s.repo.NameInEnvironment(ctx, *req.EnvironmentID, database.Name, database.ID)
		if err != nil {
			return Database{}, err
		}
		if collision {
			return Database{}, fmt.Errorf("%w: a database named %q already exists in the target environment", ErrNameConflict, database.Name)
		}
		database.EnvironmentID = *req.EnvironmentID
	} else {
		database.EnvironmentID = uuid.Nil
	}
	if req.ServerID != nil && *req.ServerID != database.ServerID {
		if *req.ServerID == uuid.Nil {
			return Database{}, fmt.Errorf("%w: server_id is required", ErrValidation)
		}
		if database.ContainerID != "" || database.Status != StatusError {
			return Database{}, ErrServerPinned
		}
		exists, err := s.repo.ServerExists(ctx, *req.ServerID, teams.ScopeFor(ctx, userID))
		if err != nil {
			return Database{}, err
		}
		if !exists {
			return Database{}, ErrServerNotFound
		}
		if database.PublicPort > 0 {
			inUse, err := s.repo.PublicPortInUse(ctx, *req.ServerID, database.PublicPort)
			if err != nil {
				return Database{}, err
			}
			if inUse {
				return Database{}, fmt.Errorf(
					"%w: public port %d is already in use on this node", ErrPortConflict, database.PublicPort)
			}
		}
		database.ServerID = *req.ServerID
	} else {
		database.ServerID = uuid.Nil
	}
	return s.repo.UpdateDatabaseTarget(ctx, database)
}

func (s *Service) claimUpdate(databaseID uuid.UUID) error {
	if !s.leases.Claim(databaseID, JobLeaseLifecycle) {
		switch s.leases.Held(databaseID) {
		case JobLeaseBackup:
			return fmt.Errorf("%w: a backup is running for this database", ErrDatabaseBusy)
		case JobLeaseRestore:
			return fmt.Errorf("%w: a restore is running for this database", ErrDatabaseBusy)
		default:
			return fmt.Errorf("%w: another operation is running for this database", ErrDatabaseBusy)
		}
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, userID, databaseID uuid.UUID) error {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return err
	}
	removed := ""
	if database.ContainerID != "" {
		if err := s.containers.Stop(ctx, database.ServerID, database.ContainerID); err != nil {
			s.logger.Warn("databases: stop before delete failed; forcing removal",
				"database_id", database.ID.String(), "error", err)
		}
		if err := s.containers.Remove(ctx, database.ServerID, database.ContainerID); err != nil &&
			!errors.Is(err, containers.ErrContainerNotFound) &&
			!errors.Is(err, containers.ErrServerNotFound) {
			return mapContainerError(err)
		}
		removed = database.ContainerID
	}
	deleted, err := s.repo.SoftDeleteDatabase(ctx, database.ID)
	if err != nil {
		return err
	}
	if deleted.ContainerID != "" && deleted.ContainerID != removed {
		s.removeContainer(ctx, deleted.ServerID, deleted.ContainerID)
	}
	s.logger.Info("databases: deleted; volume retained for the grace window",
		"database_id", database.ID.String(), "volume", database.StoragePath)
	return nil
}

func (s *Service) Start(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	database, err := s.ownedContainer(ctx, userID, databaseID)
	if err != nil {
		return Database{}, err
	}
	if err := s.claimLifecycle(database.ID); err != nil {
		return Database{}, err
	}
	defer s.leases.Release(database.ID)
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

func (s *Service) Restart(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	database, err := s.ownedContainer(ctx, userID, databaseID)
	if err != nil {
		return Database{}, err
	}
	if err := s.claimLifecycle(database.ID); err != nil {
		return Database{}, err
	}
	defer s.leases.Release(database.ID)
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

func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("databases: repository is not configured")
	}
	if s.containers == nil {
		return errors.New("databases: container service is not configured")
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

func (s *Service) claimLifecycle(databaseID uuid.UUID) error {
	if !s.leases.Claim(databaseID, JobLeaseLifecycle) {
		switch s.leases.Held(databaseID) {
		case JobLeaseBackup:
			return fmt.Errorf("%w: a backup is running for this database", ErrDatabaseBusy)
		case JobLeaseRestore:
			return fmt.Errorf("%w: a restore is running for this database", ErrDatabaseBusy)
		default:
			return fmt.Errorf("%w: another operation is running for this database", ErrDatabaseBusy)
		}
	}
	return nil
}

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

func (s *Service) waitHealthy(ctx context.Context, database Database, health Healthcheck) error {
	timeout := health.Timeout
	if s.healthTimeout > 0 {
		timeout = s.healthTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		ready, err := s.containerReady(ctx, database)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s did not become ready within %s",
				ErrHealthcheck, database.Name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.healthPoll):
		}
	}
}

func (s *Service) containerReady(ctx context.Context, database Database) (bool, error) {
	list, err := s.containers.List(ctx, database.ServerID)
	if err != nil {
		return false, mapContainerError(err)
	}
	for _, item := range list {
		if item.ID != database.ContainerID {
			continue
		}
		if item.State != "running" {
			return false, nil
		}
		return item.Health == "" || item.Health == containers.HealthHealthy, nil
	}
	return false, nil
}

func (s *Service) setStatus(ctx context.Context, database Database, status Status) (Database, error) {
	return s.repo.UpdateDatabaseStatus(ctx, database.ID, status)
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}

func (s *Service) markError(ctx context.Context, database Database, cause error) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	if _, err := s.repo.UpdateDatabaseStatus(ctx, database.ID, StatusError); err != nil {
		s.logger.Error("databases: could not mark the database as errored",
			"database_id", database.ID.String(), "error", err)
		return
	}
	s.logger.Warn("databases: operation failed",
		"database_id", database.ID.String(), "status", StatusError, "error", cause)
}

func (s *Service) ensureImage(ctx context.Context, database Database, image string) error {
	if err := s.containers.Pull(ctx, database.ServerID, image); err != nil {
		return mapContainerError(err)
	}
	return nil
}

func (s *Service) rollbackCredentials(ctx context.Context, database Database) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := s.repo.DeleteDatabaseSecrets(ctx, database.ID); err != nil {
		s.logger.Warn("databases: could not roll back credentials after a failed create",
			"database_id", database.ID.String(), "error", err)
	}
}

func (s *Service) removeContainer(ctx context.Context, serverID uuid.UUID, containerID string) {
	if strings.TrimSpace(containerID) == "" {
		return
	}
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := s.containers.Remove(ctx, serverID, containerID); err != nil &&
		!errors.Is(err, containers.ErrContainerNotFound) &&
		!errors.Is(err, containers.ErrServerNotFound) {
		s.logger.Warn("databases: could not remove container",
			"server_id", serverID.String(), "container_id", containerID, "error", err)
	}
}

func mapContainerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, containers.ErrServerNotFound):
		return fmt.Errorf("%w: %v", ErrServerNotFound, err)
	case errors.Is(err, containers.ErrContainerNotFound):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, containers.ErrPortConflict):
		return fmt.Errorf("%w: %v", ErrPortConflict, err)
	case errors.Is(err, containers.ErrAgentUnavailable):
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	case errors.Is(err, containers.ErrValidation):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return err
	}
}
