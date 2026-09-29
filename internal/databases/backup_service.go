package databases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/store"
)

// defaultLocalBackupDir is where backups land when no storage target is
// configured. GOTHAM_BACKUP_DIR overrides it, so an operator can put the
// directory on its own disk.
const (
	defaultLocalBackupDir = "data/backups"
	localBackupDirEnv     = "GOTHAM_BACKUP_DIR"
	// defaultJobTimeout bounds one dump or restore job: the node has to pull
	// an image, start a temporary engine and stream the payload.
	defaultJobTimeout = 30 * time.Minute
	// defaultSchedulerInterval is the tick of the internal cron scheduler.
	defaultSchedulerInterval = 30 * time.Second
)

// BackupConfig wires a BackupManager. Store (or an explicit Repository) and
// Containers are required for anything beyond tests; Secret is the key
// providers.SealSecret sealed the database credentials and the storage
// credentials with. LocalDir selects the directory of local backups, and
// ObjectStore overrides target resolution entirely (tests fake the network
// through it).
type BackupConfig struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is
	// set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository BackupRepository
	// DatabaseRepository serves the databases table — ownership checks and
	// sealed database credentials. Production derives it from Store; tests
	// pass their fake next to Repository.
	DatabaseRepository Repository
	// Containers creates and drives the temporary job containers through the
	// shared container service.
	Containers containers.ContainerService
	// Secret opens sealed credentials.
	Secret string
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// LocalDir is the root of local backups; empty selects
	// $GOTHAM_BACKUP_DIR or "data/backups".
	LocalDir string
	// ObjectStore, when set, serves every backup regardless of target — the
	// seam tests use to fake S3.
	ObjectStore ObjectStore
	// JobTimeout overrides defaultJobTimeout.
	JobTimeout time.Duration
	// SchedulerInterval overrides defaultSchedulerInterval.
	SchedulerInterval time.Duration
	// DisableScheduler keeps the cron loop from starting; the production
	// constructor starts it.
	DisableScheduler bool
}

// BackupService is the control-plane surface the HTTP layer depends on. It is
// implemented by BackupManager and by fakes in the route tests.
type BackupService interface {
	// CreateBackup queues a manual backup of a database the caller owns and
	// returns the row as soon as it is recorded (the job runs behind it).
	CreateBackup(ctx context.Context, userID, databaseID uuid.UUID, req CreateBackupRequest) (Backup, error)
	// ListBackups returns a database's runs, newest first.
	ListBackups(ctx context.Context, userID, databaseID uuid.UUID) ([]Backup, error)
	// GetBackup returns one run of a database the caller owns.
	GetBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) (Backup, error)
	// DeleteBackup drops the row and the stored artifact.
	DeleteBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) error
	// RestoreBackup queues a restore of a completed backup into its database
	// and returns as soon as the job is recorded.
	RestoreBackup(ctx context.Context, userID, databaseID uuid.UUID, req RestoreRequest) (RestoreResult, error)

	// ListSchedules returns a database's schedules, newest first.
	ListSchedules(ctx context.Context, userID, databaseID uuid.UUID) ([]BackupSchedule, error)
	// CreateSchedule validates the cron expression and stores a schedule.
	CreateSchedule(ctx context.Context, userID, databaseID uuid.UUID, req ScheduleRequest) (BackupSchedule, error)
	// UpdateSchedule changes the cron, target or enabled flag of a schedule.
	UpdateSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID, req ScheduleRequest) (BackupSchedule, error)
	// DeleteSchedule removes a schedule of a database the caller owns.
	DeleteSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID) error

	// ListTargets returns the caller's storage targets (never their
	// credentials).
	ListTargets(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error)
	// CreateTarget stores a target and seals the credentials it was given.
	CreateTarget(ctx context.Context, userID uuid.UUID, req TargetRequest) (BackupTarget, error)
	// UpdateTarget changes a target the caller owns; credentials are only
	// replaced when the request carries new ones, and an s3 target must keep
	// a complete access/secret pair.
	UpdateTarget(ctx context.Context, userID, targetID uuid.UUID, req TargetRequest) (BackupTarget, error)
	// DeleteTarget removes a target the caller owns.
	DeleteTarget(ctx context.Context, userID, targetID uuid.UUID) error
	// TestTarget verifies the target can be reached and its bucket exists.
	TestTarget(ctx context.Context, userID, targetID uuid.UUID) (TargetCheck, error)

	// Close stops the scheduler.
	Close() error
}

// CreateBackupRequest is the input of BackupService.CreateBackup. A zero
// TargetID keeps the artifact in the local backup directory.
type CreateBackupRequest struct {
	TargetID uuid.UUID
	// ScheduleID links a run to the schedule that triggered it; the HTTP
	// surface always sends a manual backup and leaves it zero.
	ScheduleID uuid.UUID
}

// RestoreRequest is the input of BackupService.RestoreBackup.
type RestoreRequest struct {
	BackupID uuid.UUID
}

// RestoreResult reports a queued restore.
type RestoreResult struct {
	BackupID   uuid.UUID    `json:"backup_id"`
	DatabaseID uuid.UUID    `json:"database_id"`
	Location   string       `json:"location"`
	Status     BackupStatus `json:"status"`
}

// ScheduleRequest creates or updates a backup schedule. Enabled is optional:
// nil means "enabled" on create and "unchanged" on update. TargetID uses the
// same optional pattern, and makes clearing explicit:
//
//   - nil (field absent or null): create stores no target; update leaves the
//     stored target unchanged.
//   - pointer to "": create stores no target; update clears the target, so
//     scheduled runs land in the local backup directory again.
//   - pointer to a target id: create/update store that target (it must be
//     owned by the caller).
//
// A client can therefore never clear a target by omitting the field, and an
// empty string is never silently ignored.
type ScheduleRequest struct {
	Cron     string  `json:"cron"`
	TargetID *string `json:"target_id,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

// TargetRequest creates or updates a storage target. AccessKey and SecretKey
// are sealed on write and never returned; blank values leave stored
// credentials untouched. An s3 target must always hold both halves: create
// requires them in the request, and update requires them in the request or
// already stored.
type TargetRequest struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint,omitempty"`
	Region    string `json:"region,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}

// TargetCheck is the answer of the "test connection" action.
type TargetCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// BackupManager runs backup and restore jobs: it owns the temporary-container
// flow, the storage targets and the internal cron scheduler. It embeds the
// databases Service so ownership checks, sealed credentials and container
// helpers are shared instead of re-implemented. It is safe for concurrent
// use.
type BackupManager struct {
	*Service
	backups      BackupRepository
	objects      ObjectStore
	localDir     string
	jobTimeout   time.Duration
	scheduler    *backupScheduler
	now          func() time.Time
	logger       *slog.Logger
	schedulerInt time.Duration

	// inflight holds one job per database: a dump stops the container, so a
	// second job must never race it. The scheduler reads it as well.
	mu       sync.Mutex
	inflight map[uuid.UUID]bool
}

// Compile-time guarantee that BackupManager satisfies the route-level
// contract.
var _ BackupService = (*BackupManager)(nil)

// NewBackupService builds a BackupManager from cfg. The scheduler loop is not
// started — NewDefaultBackupService starts it for the HTTP wiring — so tests
// drive ticks themselves.
func NewBackupService(cfg BackupConfig) *BackupManager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.JobTimeout
	if timeout <= 0 {
		timeout = defaultJobTimeout
	}
	interval := cfg.SchedulerInterval
	if interval <= 0 {
		interval = defaultSchedulerInterval
	}

	var backupRepo BackupRepository
	switch {
	case cfg.Repository != nil:
		backupRepo = cfg.Repository
	case cfg.Store != nil:
		backupRepo = newStoreBackupRepository(cfg.Store)
	}
	var databaseRepo Repository
	switch {
	case cfg.DatabaseRepository != nil:
		databaseRepo = cfg.DatabaseRepository
	case cfg.Store != nil:
		databaseRepo = newStoreRepository(cfg.Store)
	}

	manager := &BackupManager{
		Service: NewService(Config{
			Repository: databaseRepo,
			Containers: cfg.Containers,
			Secret:     cfg.Secret,
			Logger:     logger,
		}),
		backups:      backupRepo,
		objects:      cfg.ObjectStore,
		localDir:     resolveLocalBackupDir(cfg.LocalDir),
		jobTimeout:   timeout,
		now:          func() time.Time { return time.Now().UTC() },
		logger:       logger,
		schedulerInt: interval,
		inflight:     make(map[uuid.UUID]bool),
	}
	manager.scheduler = newBackupScheduler(manager, interval)
	return manager
}

// NewDefaultBackupService builds the production service for the HTTP wiring.
// It returns nil (a nil BackupService) when there is no database, no
// container service or the feature flag is off, so callers can pass its
// result to MountBackups unconditionally. When the scheduler is enabled it is
// started here and stopped by Close.
func NewDefaultBackupService(cfg BackupConfig) BackupService {
	if cfg.Store == nil && cfg.Repository == nil {
		return nil
	}
	if cfg.Containers == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	manager := NewBackupService(cfg)
	manager.reconcileStaleBackups()
	if !cfg.DisableScheduler {
		manager.scheduler.Start()
	}
	return manager
}

// Close stops the scheduler. It is safe to call on an unstarted manager.
func (m *BackupManager) Close() error {
	if m.scheduler != nil {
		m.scheduler.Close()
	}
	return nil
}

// reconcileStaleBackups runs once at construction. A run left in the running
// state by a crashed control plane can never finish, and a database the
// crash paused for the job may still be stopped: the sweep marks the run
// failed and best-effort resumes the container.
func (m *BackupManager) reconcileStaleBackups() {
	if m.backups == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stale, err := m.backups.ListRunningBackups(ctx)
	if err != nil {
		m.logger.Warn("databases: stale backup sweep failed", "error", err)
		return
	}
	for _, backup := range stale {
		failed := backup
		failed.Status = BackupFailed
		failed.Error = "control plane restarted during the backup"
		failed.FinishedAt = m.now()
		if _, err := m.backups.FinishBackup(ctx, failed); err != nil {
			m.logger.Warn("databases: could not fail a stale backup",
				"backup_id", backup.ID.String(), "error", err)
			continue
		}
		m.logger.Info("databases: marked stale backup failed",
			"backup_id", backup.ID.String(), "database_id", backup.DatabaseID.String())
		database, err := m.repo.GetDatabase(ctx, backup.DatabaseID)
		if err != nil {
			continue
		}
		if err := m.resumeDatabase(ctx, database); err != nil {
			m.logger.Warn("databases: could not resume a database after a stale backup",
				"database_id", database.ID.String(), "error", err)
		}
	}
}

// backupsReady reports a manager built without its repository, so the target
// and schedule surfaces fail with a clear error instead of panicking.
func (m *BackupManager) backupsReady() error {
	if m == nil || m.backups == nil {
		return errors.New("databases: backup repository is not configured")
	}
	return nil
}

// CreateBackup implements BackupService: it validates ownership and the
// target, records the run and returns while the job runs in the background.
func (m *BackupManager) CreateBackup(ctx context.Context, userID, databaseID uuid.UUID, req CreateBackupRequest) (Backup, error) {
	if !Enabled() {
		return Backup{}, ErrDisabled
	}
	database, err := m.database(ctx, userID, databaseID, true)
	if err != nil {
		return Backup{}, err
	}
	target, err := m.ownedTarget(ctx, userID, req.TargetID)
	if err != nil {
		return Backup{}, err
	}
	return m.startRun(ctx, database, target, BackupManual, req.ScheduleID)
}

// ListBackups implements BackupService.
func (m *BackupManager) ListBackups(ctx context.Context, userID, databaseID uuid.UUID) ([]Backup, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if _, err := m.database(ctx, userID, databaseID, false); err != nil {
		return nil, err
	}
	backups, err := m.backups.ListBackupsByDatabase(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	if backups == nil {
		return []Backup{}, nil
	}
	return backups, nil
}

// GetBackup implements BackupService. The database ownership check doubles as
// the lookup of another user's backup: a backup of someone else's database is
// simply not visible.
func (m *BackupManager) GetBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) (Backup, error) {
	if _, err := m.database(ctx, userID, databaseID, false); err != nil {
		return Backup{}, err
	}
	backup, err := m.backups.GetBackup(ctx, backupID)
	if err != nil {
		return Backup{}, err
	}
	if backup.DatabaseID != databaseID {
		return Backup{}, ErrNotFound
	}
	return backup, nil
}

// DeleteBackup implements BackupService: the stored artifact goes first, then
// the row, so a failed delete never leaves an unreachable object behind. It is
// a mutation of the parent database, so the team role must permit writes before
// the backup is even looked up.
func (m *BackupManager) DeleteBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) error {
	if _, err := m.database(ctx, userID, databaseID, true); err != nil {
		return err
	}
	backup, err := m.GetBackup(ctx, userID, databaseID, backupID)
	if err != nil {
		return err
	}
	if backup.Status == BackupRunning {
		return fmt.Errorf("%w: backup is still running", ErrBackupInFlight)
	}
	if backup.Location != "" {
		store, err := m.storeForLocation(ctx, backup, nil)
		if err == nil {
			if err := store.Delete(ctx, backup.Location); err != nil {
				return err
			}
		} else if !errors.Is(err, ErrValidation) {
			return err
		}
		// An unresolvable target (deleted after the run) still lets the row
		// go: there is nothing left to read.
	}
	if _, err := m.backups.DeleteBackup(ctx, backupID); err != nil {
		return err
	}
	return nil
}

// RestoreBackup implements BackupService: it validates that the backup is
// complete and belongs to the database, then queues the restore job.
func (m *BackupManager) RestoreBackup(ctx context.Context, userID, databaseID uuid.UUID, req RestoreRequest) (RestoreResult, error) {
	if !Enabled() {
		return RestoreResult{}, ErrDisabled
	}
	database, err := m.database(ctx, userID, databaseID, true)
	if err != nil {
		return RestoreResult{}, err
	}
	if req.BackupID == uuid.Nil {
		return RestoreResult{}, fmt.Errorf("%w: backup_id is required", ErrValidation)
	}
	backup, err := m.backups.GetBackup(ctx, req.BackupID)
	if err != nil {
		return RestoreResult{}, err
	}
	if backup.DatabaseID != databaseID {
		return RestoreResult{}, ErrNotFound
	}
	if backup.Status != BackupCompleted {
		return RestoreResult{}, fmt.Errorf("%w: only a completed backup can be restored", ErrBackupNotCompleted)
	}
	if !m.claim(database.ID) {
		return RestoreResult{}, ErrBackupInFlight
	}
	go m.runRestore(backup, database)
	return RestoreResult{
		BackupID:   backup.ID,
		DatabaseID: database.ID,
		Location:   backup.Location,
		Status:     BackupRunning,
	}, nil
}

// databaseCredentials opens a database's sealed credentials: the same rows
// the provisioning path writes, opened only to build a job payload.
func (m *BackupManager) databaseCredentials(ctx context.Context, database Database) (Credentials, error) {
	secrets, err := m.repo.ListSecrets(ctx, database.ID)
	if err != nil {
		return Credentials{}, err
	}
	return openCredentials(m.secret, secrets)
}

// claim marks a database as busy. Backup and restore both stop the
// container, so exactly one job may hold the claim at a time.
func (m *BackupManager) claim(databaseID uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inflight[databaseID] {
		return false
	}
	m.inflight[databaseID] = true
	return true
}

// release drops a database's claim; releasing an unclaimed id is a no-op.
func (m *BackupManager) release(databaseID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.inflight, databaseID)
}

// resolveLocalBackupDir picks the local backup directory: an explicit
// setting, then GOTHAM_BACKUP_DIR, then the default under the working
// directory.
func resolveLocalBackupDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if fromEnv := strings.TrimSpace(os.Getenv(localBackupDirEnv)); fromEnv != "" {
		return fromEnv
	}
	return defaultLocalBackupDir
}
