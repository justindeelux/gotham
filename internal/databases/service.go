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

// cleanupTimeout bounds every cancel-independent cleanup write (mark error,
// roll back credentials, remove a container). A client disconnect or proxy
// timeout must not leave a row creating or a container unmanaged, so these
// run on a context detached from the request but with their own deadline.
const cleanupTimeout = 30 * time.Second

// namePattern is the API-facing database name: a Docker-safe identifier of
// 1–63 characters. It is also what the derived SQL identifier is built from.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// CreateRequest is the input of Service.Create.
type CreateRequest struct {
	Name    string
	Engine  string
	Version string
	// EnvironmentID is the environment the database belongs to; it must be
	// in the caller's team.
	EnvironmentID uuid.UUID
	// ServerID is the node that runs the container; it must exist.
	ServerID uuid.UUID
	// PublicPort optionally publishes the engine port on the host. 0 keeps the
	// database internal to the node.
	PublicPort int32
}

// UpdateRequest carries the mutable fields of a database. A nil Name keeps
// the stored value; EnvironmentID moves the database to another environment
// of the same team and ServerID changes the node (refused while a backup,
// restore or lifecycle operation holds the database).
type UpdateRequest struct {
	Name          *string    `json:"name,omitempty"`
	EnvironmentID *uuid.UUID `json:"-"`
	ServerID      *uuid.UUID `json:"-"`
}

// DatabaseFilter scopes a list to one environment or project of the caller's
// team (the ?environment_id= and ?project_id= filters). At most one may be
// set; a foreign ID answers ErrNotFound.
type DatabaseFilter struct {
	EnvironmentID uuid.UUID
	ProjectID     uuid.UUID
}

// DatabaseService is the control-plane surface the HTTP layer depends on. It
// is implemented by Service and by fakes in the route tests.
type DatabaseService interface {
	// Create provisions a database and returns the row with its credentials.
	Create(ctx context.Context, userID uuid.UUID, req CreateRequest) (Database, Credentials, error)
	// List returns the caller's live databases, newest first, optionally
	// scoped to one environment or project.
	List(ctx context.Context, userID uuid.UUID, filter DatabaseFilter) ([]Database, error)
	// Get returns one database the caller owns (404 for anyone else's).
	Get(ctx context.Context, userID, databaseID uuid.UUID) (Database, error)
	// Credentials returns the decrypted credentials of a database the caller
	// may manage (owner/admin: plaintext secrets are not a read-only view).
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
	// Leases, when set, is the job exclusion registry shared with the backup
	// manager of the same control plane: a database whose volume a backup or
	// restore owns refuses Start/Restart, and a lifecycle action claims the
	// same lease so a job cannot start under it. Nil disables exclusion
	// (tests that do not exercise it).
	Leases *JobLeases
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
	leases        *JobLeases
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
		leases:        cfg.Leases,
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
// the credentials, writes the row, ensures the engine image is present, runs
// the container through the shared container service and waits for the
// engine's health window before reporting the database as running.
//
// Every failure after the row is written leaves a terminal error row (never a
// permanently creating one) and rolls back what it created. Writes are fenced
// on the row still being live, so a delete that lands while create is in
// flight wins and the container is removed rather than left orphaned.
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
	// Reject a public port another live database already publishes on this
	// node before writing a row: Docker would refuse the bind and, without
	// this check, the failure would surface as an opaque 500. The check is
	// best-effort against the race with a concurrent create; Docker's own
	// bind conflict is mapped to ErrPortConflict as a second line of defense.
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
	// The ID seeds the named volume, so the volume exists for every row from
	// the moment it is written.
	database.StoragePath = VolumeName(database.ID)

	stored, err := s.repo.CreateDatabase(ctx, database)
	if err != nil {
		return Database{}, Credentials{}, err
	}
	// The image comes first: credentials written before a failed pull would
	// only be rolled back again, and a fresh node has no image to run.
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
		// The agent started the container but the row could not record its
		// ID, so the control plane would never be able to manage it again.
		// Cleanup uses the ids we already hold — never a zeroed row — because
		// best-effort removal beats an orphan container on the node. A fenced
		// write (row deleted) lands here too: delete has won and the container
		// must go with it.
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
			// The row was soft-deleted while provisioning (the status write is
			// fenced on the row being live): delete has won and the container
			// must go with it.
			s.removeContainer(ctx, stored.ServerID, stored.ContainerID)
			return Database{}, Credentials{}, err
		}
		// Any other failure is transient, not a delete: keep the container so
		// the row stays recoverable (Start can still reach it) and mark the
		// row terminal error. Removing the container here would strand a
		// healthy database that a single failed UPDATE should not destroy.
		s.markError(ctx, stored, err)
		return Database{}, Credentials{}, err
	}
	return updated, credentials, nil
}

// List returns the active team's live databases, newest first. Without a team
// context it returns the creator's databases, which is the pre-teams behavior.
// A filter scopes the list to one environment or project of the team.
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

// Get returns one database of the caller's active team.
func (s *Service) Get(ctx context.Context, userID, databaseID uuid.UUID) (Database, error) {
	return s.database(ctx, userID, databaseID, false)
}

// Credentials opens the sealed credentials of a database. Plaintext secrets
// are owner/admin material, not a read-only resource view, so the team role
// must permit writes.
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

// Update renames a database of the active team, optionally moving it to
// another environment of the same team or another node. The partial unique
// index turns a collision with a live database into ErrConflict; a move
// refuses a name the target already holds (409). A node change while a
// backup, restore or lifecycle operation holds the database is refused with
// the contract's 409. Backup schedules hang off the database row, so they
// follow it to the target environment unchanged.
//
// The whole update runs under the database's job lease with a fresh read,
// so a concurrent backup, lifecycle action or second update serializes
// against it; the placement columns (environment, server) are only written
// when the request changes them, so a stale snapshot can never undo a
// committed move.
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
	// A node change while a backup or restore holds the database is refused
	// with the contract's 409 before the update claims the lease below.
	if req.ServerID != nil && *req.ServerID != database.ServerID {
		if held := s.leases.Held(databaseID); held == JobLeaseBackup || held == JobLeaseRestore {
			return Database{}, ErrDeployInFlight
		}
	}
	if err := s.claimUpdate(databaseID); err != nil {
		return Database{}, err
	}
	defer s.leases.Release(databaseID)
	// Re-read under the lease: a concurrent update, backup or lifecycle
	// action serializes against it, so this write never acts on a stale
	// snapshot.
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
		// Unchanged placement zeroes out, so the conditional write keeps
		// the freshly read value instead of a stale snapshot (a Nil UUID
		// never persists: both columns are NOT NULL).
		database.EnvironmentID = uuid.Nil
	}
	if req.ServerID != nil && *req.ServerID != database.ServerID {
		if *req.ServerID == uuid.Nil {
			return Database{}, fmt.Errorf("%w: server_id is required", ErrValidation)
		}
		// The container and its volume live on the stored node: only a
		// database that never provisioned (no container) and already
		// failed (status error, so no provision is in flight) may move.
		// A row stuck in creating with an empty container is still
		// provisioning: its Run continues against the old node.
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
		// The update already holds the lifecycle lease here, so no backup,
		// restore or concurrent lifecycle action can interleave from this
		// point on.
		// A published port binds at creation, so re-check it against the
		// target node before the row moves.
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

// claimUpdate takes the database's job lease for an update, mapping a lease
// already held by a backup, restore or lifecycle action to ErrDatabaseBusy.
// A nil registry never blocks, so a service built without exclusion keeps
// working. Callers release the lease when done.
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

// Delete stops and removes the container of a database of the active team,
// then soft-deletes the row. The named volume is never touched here: it is
// kept for VolumeRetention (7 days), after which the RetentionSweeper removes
// it and purges the row, so the data outlives both the container and the row's
// visibility but not forever.
func (s *Service) Delete(ctx context.Context, userID, databaseID uuid.UUID) error {
	database, err := s.database(ctx, userID, databaseID, true)
	if err != nil {
		return err
	}
	removed := ""
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
		removed = database.ContainerID
	}
	deleted, err := s.repo.SoftDeleteDatabase(ctx, database.ID)
	if err != nil {
		return err
	}
	// A provision that was in flight may have persisted its container id
	// after our read but before the soft delete; the returned row carries the
	// id at delete time. The row is already deleted, so removal is
	// best-effort: delete wins, the late provisioning write was fenced, and
	// this removes the container it had started rather than orphaning it.
	if deleted.ContainerID != "" && deleted.ContainerID != removed {
		s.removeContainer(ctx, deleted.ServerID, deleted.ContainerID)
	}
	s.logger.Info("databases: deleted; volume retained for the grace window",
		"database_id", database.ID.String(), "volume", database.StoragePath)
	return nil
}

// Start powers the container back on and reports the database running once the
// engine's health window passes. It refuses to run while a backup or restore
// owns the volume (409) and claims the same lease while it runs, so a job
// cannot start underneath it.
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
// Like Start it respects and claims the shared job lease.
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

// claimLifecycle claims the shared job lease for a Start/Restart, mapping a
// lease already held by a backup or restore to ErrDatabaseBusy. A nil registry
// never blocks, so a service built without exclusion keeps working.
func (s *Service) claimLifecycle(databaseID uuid.UUID) error {
	if !s.leases.Claim(databaseID, JobLeaseLifecycle) {
		switch s.leases.Held(databaseID) {
		case JobLeaseBackup:
			return fmt.Errorf("%w: a backup is running for this database", ErrDatabaseBusy)
		case JobLeaseRestore:
			return fmt.Errorf("%w: a restore is running for this database", ErrDatabaseBusy)
		default:
			// Another lifecycle action holds the lease (a double
			// Start/Restart), or the holder was released in between.
			return fmt.Errorf("%w: another operation is running for this database", ErrDatabaseBusy)
		}
	}
	return nil
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

// waitHealthy polls the container until it is actually ready to serve, bounded
// by the engine's window (or Config.HealthTimeout when set).
//
// Readiness is the container's native Docker healthcheck, which runs the
// engine's own probe command inside the container (pg_isready, mysqladmin
// ping, ...). A container that is merely "running" is not ready while that
// probe reports "starting" or "unhealthy", so status never runs ahead of an
// engine that is still initializing. A container without a healthcheck — a
// row created before databases configured one — falls back to the running
// state; new databases always carry a probe.
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

// containerReady reports whether the database container is both running and,
// when it declares a healthcheck, healthy. A container that is not listed yet
// counts as not ready; a listed container without a healthcheck falls back to
// the running state.
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

// setStatus persists a status transition and returns the updated row. The
// write is column-scoped, so it cannot revert a concurrent rename or erase a
// container id.
func (s *Service) setStatus(ctx context.Context, database Database, status Status) (Database, error) {
	return s.repo.UpdateDatabaseStatus(ctx, database.ID, status)
}

// cleanupContext returns a context detached from the request cancellation
// (context.WithoutCancel) but bounded by cleanupTimeout, so cleanup writes and
// container removals finish even after a client disconnect while still having
// a deadline of their own.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}

// markError flips a database to the error state after a failed provision or
// lifecycle action. Failures are logged only: the original error is what the
// caller sees, and a second write must not mask it. A fenced write means the
// row was deleted while the action was in flight — the intended outcome for a
// lost race with delete.
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

// ensureImage makes the engine image present on the node before create runs
// it. There is no image-inspect RPC, so create pulls unconditionally: a Docker
// pull is idempotent and skips layers already present, and it is the only way
// a fresh node (no image) can provision. The error is mapped so a failed pull
// surfaces as a clear typed failure and the caller can leave a terminal error
// row instead of a permanently-creating one.
func (s *Service) ensureImage(ctx context.Context, database Database, image string) error {
	if err := s.containers.Pull(ctx, database.ServerID, image); err != nil {
		return mapContainerError(err)
	}
	return nil
}

// rollbackCredentials removes the sealed credentials of a database whose
// create is being abandoned. Failures are logged only: the create error is
// what the caller sees, and a stray ciphertext row is not a credential leak.
func (s *Service) rollbackCredentials(ctx context.Context, database Database) {
	ctx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := s.repo.DeleteDatabaseSecrets(ctx, database.ID); err != nil {
		s.logger.Warn("databases: could not roll back credentials after a failed create",
			"database_id", database.ID.String(), "error", err)
	}
}

// removeContainer best-effort removes a container that a failed or fenced
// create would otherwise leave unmanaged. A missing container or node is
// success; anything else is logged because the caller already has the primary
// error.
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
