package databases

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/providers"
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
	// replaced when the request carries new ones.
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
// nil means "enabled" on create and "unchanged" on update.
type ScheduleRequest struct {
	Cron     string `json:"cron"`
	TargetID string `json:"target_id,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// TargetRequest creates or updates a storage target. AccessKey and SecretKey
// are sealed on write and never returned; empty leaves stored credentials
// untouched.
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

// CreateBackup implements BackupService: it validates ownership and the
// target, records the run and returns while the job runs in the background.
func (m *BackupManager) CreateBackup(ctx context.Context, userID, databaseID uuid.UUID, req CreateBackupRequest) (Backup, error) {
	if !Enabled() {
		return Backup{}, ErrDisabled
	}
	database, err := m.database(ctx, userID, databaseID)
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
	if _, err := m.database(ctx, userID, databaseID); err != nil {
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
	if _, err := m.database(ctx, userID, databaseID); err != nil {
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
// the row, so a failed delete never leaves an unreachable object behind.
func (m *BackupManager) DeleteBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) error {
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
	database, err := m.database(ctx, userID, databaseID)
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

// ListSchedules implements BackupService.
func (m *BackupManager) ListSchedules(ctx context.Context, userID, databaseID uuid.UUID) ([]BackupSchedule, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return nil, err
	}
	schedules, err := m.backups.ListBackupSchedulesByDatabase(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	if schedules == nil {
		return []BackupSchedule{}, nil
	}
	return schedules, nil
}

// CreateSchedule implements BackupService. The expression is validated and
// the first run computed before anything is written, so an invalid cron
// never reaches the table.
func (m *BackupManager) CreateSchedule(ctx context.Context, userID, databaseID uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return BackupSchedule{}, err
	}
	cron := strings.TrimSpace(req.Cron)
	next, err := nextCronTime(cron, m.now(), time.Local)
	if err != nil {
		return BackupSchedule{}, err
	}
	targetID, err := m.ownedTargetID(ctx, userID, req.TargetID)
	if err != nil {
		return BackupSchedule{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := m.now()
	schedule := BackupSchedule{
		ID:         uuid.New(),
		DatabaseID: databaseID,
		Cron:       cron,
		TargetID:   targetID,
		Enabled:    enabled,
		NextRunAt:  next,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	created, err := m.backups.CreateBackupSchedule(ctx, schedule)
	if err != nil {
		return BackupSchedule{}, err
	}
	m.logger.Info("databases: backup schedule created",
		"schedule_id", created.ID.String(), "database_id", databaseID.String(),
		"cron", cron, "next_run_at", created.NextRunAt.Format(time.RFC3339))
	return created, nil
}

// UpdateSchedule implements BackupService. The cron expression and the
// computed next run are always recomputed, so a schedule cannot drift from
// its expression.
func (m *BackupManager) UpdateSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return BackupSchedule{}, err
	}
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return BackupSchedule{}, err
	}
	if schedule.DatabaseID != databaseID {
		return BackupSchedule{}, ErrNotFound
	}
	cron := strings.TrimSpace(req.Cron)
	next, err := nextCronTime(cron, m.now(), time.Local)
	if err != nil {
		return BackupSchedule{}, err
	}
	targetID := schedule.TargetID
	if strings.TrimSpace(req.TargetID) != "" {
		if targetID, err = m.ownedTargetID(ctx, userID, req.TargetID); err != nil {
			return BackupSchedule{}, err
		}
	}
	enabled := schedule.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	schedule.Cron = cron
	schedule.TargetID = targetID
	schedule.Enabled = enabled
	schedule.NextRunAt = next
	schedule.UpdatedAt = m.now()
	return m.backups.UpdateBackupSchedule(ctx, schedule)
}

// DeleteSchedule implements BackupService.
func (m *BackupManager) DeleteSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID) error {
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return err
	}
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return err
	}
	if schedule.DatabaseID != databaseID {
		return ErrNotFound
	}
	if _, err := m.backups.DeleteBackupSchedule(ctx, scheduleID); err != nil {
		return err
	}
	return nil
}

// ListTargets implements BackupService.
func (m *BackupManager) ListTargets(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	targets, err := m.backups.ListBackupTargetsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		return []BackupTarget{}, nil
	}
	return targets, nil
}

// CreateTarget implements BackupService: configuration in the clear,
// credentials sealed.
func (m *BackupManager) CreateTarget(ctx context.Context, userID uuid.UUID, req TargetRequest) (BackupTarget, error) {
	if err := m.backupsReady(); err != nil {
		return BackupTarget{}, err
	}
	if userID == uuid.Nil {
		return BackupTarget{}, ErrNotFound
	}
	target := BackupTarget{
		ID:     uuid.New(),
		UserID: userID,
	}
	if err := applyTargetRequest(&target, req); err != nil {
		return BackupTarget{}, err
	}
	now := m.now()
	target.CreatedAt, target.UpdatedAt = now, now
	created, err := m.backups.CreateBackupTarget(ctx, target)
	if err != nil {
		return BackupTarget{}, err
	}
	if err := m.sealTargetCredentials(ctx, created, req); err != nil {
		return BackupTarget{}, err
	}
	return created, nil
}

// UpdateTarget implements BackupService. Credentials are replaced only when
// the request carries new ones, so a UI that resends the masked value cannot
// wipe a stored key.
func (m *BackupManager) UpdateTarget(ctx context.Context, userID, targetID uuid.UUID, req TargetRequest) (BackupTarget, error) {
	target, err := m.ownedTarget(ctx, userID, targetID)
	if err != nil {
		return BackupTarget{}, err
	}
	if err := applyTargetRequest(target, req); err != nil {
		return BackupTarget{}, err
	}
	target.UpdatedAt = m.now()
	updated, err := m.backups.UpdateBackupTarget(ctx, *target)
	if err != nil {
		return BackupTarget{}, err
	}
	if err := m.sealTargetCredentials(ctx, updated, req); err != nil {
		return BackupTarget{}, err
	}
	return updated, nil
}

// DeleteTarget implements BackupService. Backups that used the target keep
// their location; only their ability to be read back from S3 goes with the
// credentials, which the API surface documents.
func (m *BackupManager) DeleteTarget(ctx context.Context, userID, targetID uuid.UUID) error {
	if _, err := m.ownedTarget(ctx, userID, targetID); err != nil {
		return err
	}
	if _, err := m.backups.DeleteBackupTarget(ctx, targetID, userID); err != nil {
		return err
	}
	return nil
}

// TestTarget implements BackupService: it builds the real client and asks
// whether the bucket is reachable. The answer never includes credentials.
func (m *BackupManager) TestTarget(ctx context.Context, userID, targetID uuid.UUID) (TargetCheck, error) {
	target, err := m.ownedTarget(ctx, userID, targetID)
	if err != nil {
		return TargetCheck{}, err
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
	}
	s3, ok := store.(*s3Store)
	if !ok {
		return TargetCheck{OK: true, Message: "local backup directory is ready"}, nil
	}
	exists, err := s3.bucketExists(ctx)
	if err != nil {
		return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
	}
	if !exists {
		return TargetCheck{OK: false, Message: "bucket " + target.Bucket + " does not exist or is not accessible"}, nil
	}
	return TargetCheck{OK: true, Message: "connected to " + target.Bucket}, nil
}

// --- job execution ---------------------------------------------------------

// backupsReady reports a manager built without its repository, so the target
// and schedule surfaces fail with a clear error instead of panicking.
func (m *BackupManager) backupsReady() error {
	if m == nil || m.backups == nil {
		return errors.New("databases: backup repository is not configured")
	}
	return nil
}

// startRun records a backup row, claims the database's single-flight slot and
// spawns the job. It is shared by the HTTP surface and the scheduler.
func (m *BackupManager) startRun(ctx context.Context, database Database, target *BackupTarget, typ BackupType, scheduleID uuid.UUID) (Backup, error) {
	if err := m.backupsReady(); err != nil {
		return Backup{}, err
	}
	if !m.claim(database.ID) {
		return Backup{}, ErrBackupInFlight
	}
	backup := Backup{
		ID:         uuid.New(),
		DatabaseID: database.ID,
		ScheduleID: scheduleID,
		Type:       typ,
		Status:     BackupRunning,
		CreatedAt:  m.now(),
	}
	if target != nil {
		backup.TargetID = target.ID
	}
	stored, err := m.backups.CreateBackup(ctx, backup)
	if err != nil {
		m.release(database.ID)
		return Backup{}, err
	}
	var targetCopy *BackupTarget
	if target != nil {
		targetCopy = &BackupTarget{}
		*targetCopy = *target
	}
	go m.runBackup(stored, database, targetCopy)
	return stored, nil
}

// runBackup is the whole dump job: stop the database, dump it from a
// temporary container, compress and store the bytes, then put the database
// back the way it was. Any failure is recorded on the row.
func (m *BackupManager) runBackup(backup Backup, database Database, target *BackupTarget) {
	defer m.release(database.ID)

	ctx, cancel := context.WithTimeout(context.Background(), m.jobTimeout)
	defer cancel()

	location, size, containerID, err := m.dump(ctx, database, target, backup.ID)
	finished := backup
	finished.ContainerID = containerID
	finished.FinishedAt = m.now()
	if err != nil {
		finished.Status = BackupFailed
		finished.Error = boundedDiag(err.Error())
		m.logger.Error("databases: backup failed",
			"backup_id", backup.ID.String(), "database_id", database.ID.String(),
			"type", string(backup.Type), "error", err)
	} else {
		finished.Status = BackupCompleted
		finished.Location = location
		finished.Size = size
		m.logger.Info("databases: backup completed",
			"backup_id", backup.ID.String(), "database_id", database.ID.String(),
			"type", string(backup.Type), "size", size, "location", location)
	}

	// The job context may already be expired; the row write gets its own.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishCancel()
	if _, ferr := m.backups.FinishBackup(finishCtx, finished); ferr != nil {
		m.logger.Error("databases: could not record the backup outcome",
			"backup_id", backup.ID.String(), "error", ferr)
	}
}

// runRestore is the restore job: download the artifact, stage it onto the
// database volume in chunks and apply it from a temporary container.
func (m *BackupManager) runRestore(backup Backup, database Database) {
	defer m.release(database.ID)

	ctx, cancel := context.WithTimeout(context.Background(), m.jobTimeout)
	defer cancel()

	if err := m.restore(ctx, database, backup); err != nil {
		m.logger.Error("databases: restore failed",
			"backup_id", backup.ID.String(), "database_id", database.ID.String(), "error", err)
		return
	}
	m.logger.Info("databases: restore completed",
		"backup_id", backup.ID.String(), "database_id", database.ID.String())
}

// dump stops the database, runs the engine's dump job on the node, compresses
// the payload into a temporary file and uploads it to the target. It returns
// the artifact location, its compressed size and the temporary container id.
func (m *BackupManager) dump(ctx context.Context, database Database, target *BackupTarget, runID uuid.UUID) (string, int64, string, error) {
	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return "", 0, "", fmt.Errorf("%w: engine %q has no backup engine", ErrValidation, database.Engine)
	}
	credentials, err := m.databaseCredentials(ctx, database)
	if err != nil {
		return "", 0, "", err
	}
	wasRunning, err := m.pauseDatabase(ctx, database)
	if err != nil {
		return "", 0, "", err
	}
	if wasRunning {
		defer func() {
			if resumeErr := m.resumeDatabase(context.Background(), database); resumeErr != nil {
				m.logger.Error("databases: could not restart the database after the backup",
					"database_id", database.ID.String(), "error", resumeErr)
			}
		}()
	}

	options, err := engine.DumpOptions(database, credentials, runID.String())
	if err != nil {
		return "", 0, "", err
	}

	temp, err := os.CreateTemp("", "gotham-backup-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("databases: create staging file: %w", err)
	}
	defer func() {
		name := temp.Name()
		_ = temp.Close()
		_ = os.Remove(name)
	}()
	compressor := gzip.NewWriter(temp)
	collector := newJobCollector(runID.String(), compressor)

	containerID, jobErr := m.runJob(ctx, database.ServerID, options, collector)
	if jobErr == nil {
		jobErr = collector.Result()
	}
	if closeErr := compressor.Close(); closeErr != nil && jobErr == nil {
		jobErr = fmt.Errorf("databases: finish compression: %w", closeErr)
	}
	if syncErr := temp.Sync(); syncErr != nil && jobErr == nil {
		jobErr = fmt.Errorf("databases: flush staging file: %w", syncErr)
	}
	if jobErr != nil {
		return "", 0, containerID, jobErr
	}
	// The stored artifact is the compressed stream, so the size that matters
	// is the file's — the collector counted the raw payload it decoded.
	info, statErr := temp.Stat()
	if statErr != nil {
		return "", 0, containerID, fmt.Errorf("databases: stat staging file: %w", statErr)
	}
	size := info.Size()
	// Re-read the compressed bytes: the file was written sequentially.
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return "", 0, containerID, fmt.Errorf("databases: rewind staging file: %w", err)
	}
	store, key, err := m.storeAndKey(ctx, target, database, runID)
	if err != nil {
		return "", 0, containerID, err
	}
	location, err := store.Put(ctx, key, temp, size)
	if err != nil {
		return "", 0, containerID, err
	}
	return location, size, containerID, nil
}

// restore downloads the artifact, stages it onto the volume in bounded
// chunks and runs the engine's restore job, with the database stopped
// throughout so the temporary container is the only writer.
func (m *BackupManager) restore(ctx context.Context, database Database, backup Backup) error {
	store, err := m.storeForLocation(ctx, backup, &database)
	if err != nil {
		return err
	}
	artifact, err := store.Get(ctx, backup.Location)
	if err != nil {
		return err
	}
	defer func() { _ = artifact.Close() }()

	wasRunning, err := m.pauseDatabase(ctx, database)
	if err != nil {
		return err
	}
	if wasRunning {
		defer func() {
			if resumeErr := m.resumeDatabase(context.Background(), database); resumeErr != nil {
				m.logger.Error("databases: could not restart the database after the restore",
					"database_id", database.ID.String(), "error", resumeErr)
			}
		}()
	}

	staged, err := stagingPath(database, backup.ID)
	if err != nil {
		return err
	}
	if err := m.stageArtifact(ctx, database, backup.ID, artifact, staged); err != nil {
		return err
	}

	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return fmt.Errorf("%w: engine %q has no backup engine", ErrValidation, database.Engine)
	}
	credentials, err := m.databaseCredentials(ctx, database)
	if err != nil {
		return err
	}
	options, err := engine.RestoreOptions(database, credentials, backup.ID.String(), staged)
	if err != nil {
		return err
	}
	collector := newJobCollector(backup.ID.String(), nil)
	if _, err := m.runJob(ctx, database.ServerID, options, collector); err != nil {
		return err
	}
	return collector.Result()
}

// stageArtifact writes the artifact onto the database volume one bounded
// chunk at a time. Each chunk is a temporary container whose command carries
// a base64 payload: the agent contract has no file-transfer RPC, and a
// command argument is the largest channel that survives it (ARG_MAX is
// 2 MiB, the gRPC cap 4 MiB).
func (m *BackupManager) stageArtifact(ctx context.Context, database Database, runID uuid.UUID, data io.Reader, stagedPath string) error {
	buffer := make([]byte, stageChunkBytes)
	for index := 0; ; index++ {
		n, err := io.ReadFull(data, buffer)
		if n > 0 {
			if stageErr := m.stageChunk(ctx, database, runID, buffer[:n], stagedPath, index); stageErr != nil {
				return stageErr
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil
		default:
			return fmt.Errorf("databases: read backup artifact: %w", err)
		}
	}
}

// stageChunk appends (or, for index 0, replaces) one base64 chunk of the
// artifact inside the database volume and waits for the container to finish.
func (m *BackupManager) stageChunk(ctx context.Context, database Database, runID uuid.UUID, chunk []byte, stagedPath string, index int) error {
	encoded := base64.StdEncoding.EncodeToString(chunk)
	operator := ">>"
	if index == 0 {
		operator = ">"
	}
	directory := path.Dir(stagedPath)
	if !safeJobPath(directory) || !safeJobPath(stagedPath) {
		return fmt.Errorf("%w: staging path contains characters a shell argument cannot carry", ErrValidation)
	}
	// The chunk never contains shell metacharacters: base64 is alphanumerics
	// plus "+" "/" "=", and both paths are built from a validated mount path
	// and a UUID.
	script := fmt.Sprintf(
		"id=\"$GOTHAM_RUN_ID\"\n"+
			"if mkdir -p '%s' && printf '%%s' '%s' %s '%s'; then\n"+
			"  printf 'GOTHAM-BACKUP-START %%s\\nGOTHAM-BACKUP-PAYLOAD %%s 0\\nGOTHAM-BACKUP-END %%s ok\\n' \"$id\" \"$id\" \"$id\"\n"+
			"else\n"+
			"  printf 'GOTHAM-BACKUP-START %%s\\nGOTHAM-BACKUP-END %%s fail 3\\n' \"$id\" \"$id\"\n"+
			"fi\n",
		directory, encoded, operator, stagedPath)

	engine, _, err := parseEngine(database.Engine)
	if err != nil {
		return err
	}
	options := containers.RunOptions{
		Image:   engine.Image(database.Version),
		Name:    tempJobName(roleStage+"-"+strconv.Itoa(index), runID.String()),
		Env:     []string{"GOTHAM_RUN_ID=" + runID.String()},
		Command: []string{"sh", "-c", script},
		Labels: map[string]string{
			labelManaged:    "true",
			labelDatabaseID: database.ID.String(),
			labelEngine:     database.Engine,
			labelRole:       roleStage,
			labelBackupID:   runID.String(),
		},
		Volumes: []string{database.StoragePath + ":" + engine.VolumeSpec().MountPath},
	}
	collector := newJobCollector(runID.String(), nil)
	if _, err := m.runJob(ctx, database.ServerID, options, collector); err != nil {
		return err
	}
	return collector.Result()
}

// runJob starts a temporary container, feeds its log stream into sink and
// returns the container id. The container is removed afterwards, success or
// not: a job container exists only while its job runs.
func (m *BackupManager) runJob(ctx context.Context, serverID uuid.UUID, options containers.RunOptions, sink io.Writer) (string, error) {
	containerID, err := m.containers.Run(ctx, serverID, options)
	if err != nil {
		return "", mapContainerError(err)
	}
	defer m.removeJobContainer(serverID, containerID)

	logs, err := m.containers.Logs(ctx, serverID, containerID, true)
	if err != nil {
		return containerID, mapContainerError(err)
	}
	// The channel closes when Docker ends the stream — with follow, that is
	// when the container exits — or when the job context expires.
	for chunk := range logs {
		if sink != nil {
			// jobCollector records write failures internally, so a failed
			// write must not stop the drain: the channel has to be emptied
			// for the agent goroutine to finish.
			_, _ = sink.Write(chunk)
		}
	}
	return containerID, ctx.Err()
}

// removeJobContainer force-removes a temporary container with its own
// context: the job's context may already be gone, and a leaked container
// would hold the volume's data directory.
func (m *BackupManager) removeJobContainer(serverID uuid.UUID, containerID string) {
	if containerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.containers.Remove(ctx, serverID, containerID); err != nil && !errors.Is(err, containers.ErrContainerNotFound) {
		m.logger.Warn("databases: could not remove the job container",
			"container_id", containerID, "error", err)
	}
}

// pauseDatabase stops the database container before a job mounts its volume,
// reporting whether it was running. A database without a container (row
// created, container gone) needs no pause.
func (m *BackupManager) pauseDatabase(ctx context.Context, database Database) (bool, error) {
	if database.ContainerID == "" {
		return false, nil
	}
	list, err := m.containers.List(ctx, database.ServerID)
	if err != nil {
		return false, mapContainerError(err)
	}
	running := false
	for _, item := range list {
		if item.ID == database.ContainerID {
			running = item.State == "running"
			break
		}
	}
	if !running {
		return false, nil
	}
	if err := m.containers.Stop(ctx, database.ServerID, database.ContainerID); err != nil {
		return false, mapContainerError(err)
	}
	return true, nil
}

// resumeDatabase starts the database container again. It runs with a fresh
// context so a cancelled job still brings the database back up.
func (m *BackupManager) resumeDatabase(ctx context.Context, database Database) error {
	if database.ContainerID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := m.containers.Start(ctx, database.ServerID, database.ContainerID); err != nil {
		return mapContainerError(err)
	}
	return nil
}

// --- storage ---------------------------------------------------------------

// storeAndKey resolves the store a new artifact goes to and the key it is
// written under. The key is identical for every target — "s3://" locations
// carry the bucket and "file://" locations the absolute path, and a target
// prefix only ever applies inside S3.
func (m *BackupManager) storeAndKey(
	ctx context.Context,
	target *BackupTarget,
	database Database,
	runID uuid.UUID,
) (ObjectStore, string, error) {
	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return nil, "", fmt.Errorf("%w: engine %q has no backup engine", ErrValidation, database.Engine)
	}
	key := fmt.Sprintf("databases/%s/%s.%s.gz", database.ID, runID, engine.DumpExtension())
	if m.objects != nil {
		return m.objects, key, nil
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return nil, "", err
	}
	return store, key, nil
}

// storeForTarget resolves the store of one target; a nil target (or a local
// one) is the control plane's own backup directory.
func (m *BackupManager) storeForTarget(ctx context.Context, target *BackupTarget) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	if target == nil || target.Kind == TargetLocal {
		return newLocalStore(m.localDir)
	}
	if target.Kind != TargetS3 {
		return nil, fmt.Errorf("%w: unsupported target kind %q", ErrValidation, target.Kind)
	}
	config, err := m.openTarget(ctx, *target)
	if err != nil {
		return nil, err
	}
	return newS3Store(config)
}

// storeForLocation resolves the store that can read a recorded location. The
// scheme decides: local files need no target, an s3:// location needs the
// target its credentials were sealed with.
func (m *BackupManager) storeForLocation(ctx context.Context, backup Backup, database *Database) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	switch {
	case strings.HasPrefix(backup.Location, locationFilePrefix):
		return newLocalStore(m.localDir)
	case strings.HasPrefix(backup.Location, locationS3Prefix):
		if backup.TargetID == uuid.Nil {
			return nil, fmt.Errorf("%w: the storage target of this backup no longer exists", ErrValidation)
		}
		target, err := m.backups.GetBackupTarget(ctx, backup.TargetID)
		if err != nil {
			return nil, err
		}
		if database != nil && target.UserID != database.UserID {
			return nil, ErrNotFound
		}
		return m.storeForTarget(ctx, &target)
	default:
		return nil, fmt.Errorf("%w: unknown backup location %q", ErrValidation, backup.Location)
	}
}

// openTarget opens a target's sealed credentials and assembles the plaintext
// configuration of its client. The values returned here are never logged and
// never leave this call chain.
func (m *BackupManager) openTarget(ctx context.Context, target BackupTarget) (s3Config, error) {
	secrets, err := m.backups.ListTargetSecrets(ctx, target.ID)
	if err != nil {
		return s3Config{}, err
	}
	accessKey, secretKey, err := openTargetSecrets(m.secret, secrets)
	if err != nil {
		return s3Config{}, err
	}
	return s3Config{
		endpoint:  target.Endpoint,
		region:    target.Region,
		bucket:    target.Bucket,
		prefix:    target.Prefix,
		accessKey: accessKey,
		secretKey: secretKey,
	}, nil
}

// --- credentials and targets -----------------------------------------------

// sealTargetSecrets seals the halves of an S3 login with AES-256-GCM
// (providers.SealSecret). Empty values are skipped so a partial update
// replaces only what it carries.
func sealTargetSecrets(secret string, targetID uuid.UUID, accessKey, secretKey string) ([]TargetSecret, error) {
	pairs := [][2]string{
		{targetSecretAccessKey, accessKey},
		{targetSecretSecretKey, secretKey},
	}
	sealed := make([]TargetSecret, 0, len(pairs))
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		ciphertext, err := providers.SealSecret(secret, pair[1])
		if err != nil {
			return nil, fmt.Errorf("databases: seal %s: %w", pair[0], err)
		}
		sealed = append(sealed, TargetSecret{
			TargetID:   targetID,
			Key:        pair[0],
			Ciphertext: ciphertext,
		})
	}
	return sealed, nil
}

// openTargetSecrets reverses sealTargetSecrets. Unknown keys are ignored so a
// future credential can travel through the same table.
func openTargetSecrets(secret string, secrets []TargetSecret) (string, string, error) {
	var accessKey, secretKey string
	for _, item := range secrets {
		value, err := providers.OpenSecret(secret, item.Ciphertext)
		if err != nil {
			return "", "", fmt.Errorf("databases: open %s: %w", item.Key, err)
		}
		switch item.Key {
		case targetSecretAccessKey:
			accessKey = value
		case targetSecretSecretKey:
			secretKey = value
		}
	}
	return accessKey, secretKey, nil
}

// sealTargetCredentials writes the credentials a request carried, replacing
// whatever is stored under the same key. A request without credentials is a
// no-op, so resending a masked value cannot wipe a stored key.
func (m *BackupManager) sealTargetCredentials(ctx context.Context, target BackupTarget, req TargetRequest) error {
	if req.AccessKey == "" && req.SecretKey == "" {
		return nil
	}
	sealed, err := sealTargetSecrets(m.secret, target.ID, req.AccessKey, req.SecretKey)
	if err != nil {
		return err
	}
	for _, item := range sealed {
		if _, err := m.backups.UpsertTargetSecret(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// applyTargetRequest copies the request onto the target, keeping stored
// values for fields the request leaves empty, and validates the resulting
// configuration. Credentials are handled separately by the callers.
func applyTargetRequest(target *BackupTarget, req TargetRequest) error {
	if name := strings.TrimSpace(req.Name); name != "" {
		target.Name = name
	}
	if kind := strings.ToLower(strings.TrimSpace(req.Kind)); kind != "" {
		switch TargetKind(kind) {
		case TargetS3, TargetLocal:
			target.Kind = TargetKind(kind)
		default:
			return fmt.Errorf("%w: unsupported target kind %q (supported: s3, local)", ErrValidation, req.Kind)
		}
	}
	if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
		target.Endpoint = endpoint
	}
	if region := strings.TrimSpace(req.Region); region != "" {
		target.Region = region
	}
	if bucket := strings.TrimSpace(req.Bucket); bucket != "" {
		target.Bucket = bucket
	}
	if prefix := strings.TrimSpace(req.Prefix); prefix != "" {
		target.Prefix = prefix
	}

	if strings.TrimSpace(target.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if target.Kind == "" {
		return fmt.Errorf("%w: kind is required (s3 or local)", ErrValidation)
	}
	if target.Kind == TargetS3 {
		if strings.TrimSpace(target.Endpoint) == "" {
			return fmt.Errorf("%w: endpoint is required for an s3 target", ErrValidation)
		}
		if strings.TrimSpace(target.Bucket) == "" {
			return fmt.Errorf("%w: bucket is required for an s3 target", ErrValidation)
		}
	}
	return nil
}

// ownedTarget loads a target the caller owns. A zero target id means "the
// default local store" and resolves to a nil target instead of an error.
func (m *BackupManager) ownedTarget(ctx context.Context, userID, targetID uuid.UUID) (*BackupTarget, error) {
	if targetID == uuid.Nil {
		return nil, nil
	}
	target, err := m.backups.GetBackupTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target.UserID != userID {
		return nil, ErrNotFound
	}
	return &target, nil
}

// ownedTargetID is ownedTarget for the string ids the API carries.
func (m *BackupManager) ownedTargetID(ctx context.Context, userID uuid.UUID, raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	targetID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid target id", ErrValidation)
	}
	if _, err := m.ownedTarget(ctx, userID, targetID); err != nil {
		return uuid.Nil, err
	}
	return targetID, nil
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
