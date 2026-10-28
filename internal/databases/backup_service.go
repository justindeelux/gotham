package databases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/store"
)

const (
	defaultLocalBackupDir    = "data/backups"
	localBackupDirEnv        = "GOTHAM_BACKUP_DIR"
	defaultJobTimeout        = 30 * time.Minute
	defaultSchedulerInterval = 30 * time.Second
	defaultBackupListLimit   = 50
	maxBackupListLimit       = 200

	// DefaultMaxBackupRunSeconds is the documented fallback when a running
	// backup has no live control-plane lease. Owners can tune it per schedule
	// between 15 minutes and 24 hours.
	DefaultMaxBackupRunSeconds = 2 * 60 * 60
	MinMaxBackupRunSeconds     = 15 * 60
	MaxMaxBackupRunSeconds     = 24 * 60 * 60
)

type BackupConfig struct {
	Store              *store.Store
	Repository         BackupRepository
	DatabaseRepository Repository
	Containers         containers.ContainerService
	Secret             string
	Logger             *slog.Logger
	LocalDir           string
	ObjectStore        ObjectStore
	JobTimeout         time.Duration
	SchedulerInterval  time.Duration
	DisableScheduler   bool
	Notifier           BackupNotifier
	Leases             *JobLeases
}

type BackupService interface {
	CreateBackup(ctx context.Context, userID, databaseID uuid.UUID, req CreateBackupRequest) (Backup, error)
	ListBackups(ctx context.Context, userID, databaseID uuid.UUID, limit int) ([]Backup, error)
	GetBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) (Backup, error)
	DeleteBackup(ctx context.Context, userID, databaseID, backupID uuid.UUID) error
	RestoreBackup(ctx context.Context, userID, databaseID uuid.UUID, req RestoreRequest) (RestoreResult, error)
	ListRestores(ctx context.Context, userID, databaseID uuid.UUID, limit int) ([]Restore, error)

	ListSchedules(ctx context.Context, userID, databaseID uuid.UUID) ([]BackupSchedule, error)
	CreateSchedule(ctx context.Context, userID, databaseID uuid.UUID, req ScheduleRequest) (BackupSchedule, error)
	UpdateSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID, req ScheduleRequest) (BackupSchedule, error)
	DeleteSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID) error

	ListTargets(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error)
	CreateTarget(ctx context.Context, userID uuid.UUID, req TargetRequest) (BackupTarget, error)
	UpdateTarget(ctx context.Context, userID, targetID uuid.UUID, req TargetRequest) (BackupTarget, error)
	DeleteTarget(ctx context.Context, userID, targetID uuid.UUID) error
	TestTarget(ctx context.Context, userID, targetID uuid.UUID) (TargetCheck, error)

	Close() error
}

type CreateBackupRequest struct {
	TargetID   uuid.UUID
	ScheduleID uuid.UUID
}

type RestoreRequest struct {
	BackupID uuid.UUID
}

type RestoreResult struct {
	RestoreID  uuid.UUID    `json:"restore_id"`
	BackupID   uuid.UUID    `json:"backup_id"`
	DatabaseID uuid.UUID    `json:"database_id"`
	Location   string       `json:"location"`
	Status     BackupStatus `json:"status"`
}

type ScheduleRequest struct {
	Cron          string  `json:"cron"`
	TargetID      *string `json:"target_id,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
	MaxRunSeconds *int    `json:"max_run_seconds,omitempty"`
}

type TargetRequest struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Endpoint  string  `json:"endpoint,omitempty"`
	Region    *string `json:"region,omitempty"`
	Bucket    string  `json:"bucket,omitempty"`
	Prefix    *string `json:"prefix,omitempty"`
	AccessKey string  `json:"access_key,omitempty"`
	SecretKey string  `json:"secret_key,omitempty"`
}

type TargetCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

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
	notifier     BackupNotifier

	leases *JobLeases
}

var _ BackupService = (*BackupManager)(nil)

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

	leases := cfg.Leases
	if leases == nil {
		leases = NewJobLeases()
	}
	manager := &BackupManager{
		Service: NewService(Config{
			Repository: databaseRepo,
			Containers: cfg.Containers,
			Secret:     cfg.Secret,
			Logger:     logger,
			Leases:     leases,
		}),
		backups:      backupRepo,
		objects:      cfg.ObjectStore,
		localDir:     resolveLocalBackupDir(cfg.LocalDir),
		jobTimeout:   timeout,
		now:          func() time.Time { return time.Now().UTC() },
		logger:       logger,
		schedulerInt: interval,
		notifier:     cfg.Notifier,
		leases:       leases,
	}
	manager.scheduler = newBackupScheduler(manager, interval)
	return manager
}

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
	go manager.sweepJobContainers()
	if !cfg.DisableScheduler {
		manager.scheduler.Start()
	}
	return manager
}

func (m *BackupManager) Close() error {
	if m.scheduler != nil {
		m.scheduler.Close()
	}
	return nil
}

// BackupRunActive reports whether this control plane still holds the lease for
// the exact backup run. The backup-protection observer uses it as lease
// evidence; it never starts or cancels a job itself.
func (m *BackupManager) BackupRunActive(databaseID, backupID uuid.UUID) bool {
	return m != nil && backupID != uuid.Nil &&
		m.leases.Held(databaseID) == JobLeaseBackup &&
		m.leases.Run(databaseID) == backupID
}

func normalizeMaxRunSeconds(value *int, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	if *value < MinMaxBackupRunSeconds || *value > MaxMaxBackupRunSeconds {
		return 0, fmt.Errorf("%w: max_run_seconds must be between %d and %d",
			ErrValidation, MinMaxBackupRunSeconds, MaxMaxBackupRunSeconds)
	}
	return *value, nil
}

func (m *BackupManager) reconcileStaleBackups() {
	if m.backups == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stale, backupErr := m.backups.ListRunningBackups(ctx)
	if backupErr != nil {
		m.logger.Warn("databases: stale backup sweep failed", "error", backupErr)
	}
	restores, restoreErr := m.backups.ListRunningRestores(ctx)
	if restoreErr != nil {
		m.logger.Warn("databases: stale restore sweep failed", "error", restoreErr)
	}
	restoring := make(map[uuid.UUID]bool, len(restores))
	for _, restore := range restores {
		restoring[restore.DatabaseID] = true
	}

	if backupErr == nil {
		for _, backup := range stale {
			if kind := m.leases.Held(backup.DatabaseID); kind != "" {
				m.logger.Warn("databases: not sweeping a backup with a live job",
					"backup_id", backup.ID.String(), "lease", string(kind))
				continue
			}
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
			database, ok := m.releaseInterrupted(ctx, backup.DatabaseID)
			if ok && backup.WasRunning && restoreErr == nil && !restoring[backup.DatabaseID] {
				m.resumeAfterSweep(ctx, database)
			}
		}
	}

	if restoreErr != nil {
		return
	}
	for _, restore := range restores {
		if kind := m.leases.Held(restore.DatabaseID); kind != "" {
			m.logger.Warn("databases: not sweeping a restore with a live job",
				"restore_id", restore.ID.String(), "lease", string(kind))
			continue
		}
		failed := restore
		failed.Status = RestoreFailed
		failed.Error = "control plane restarted during the restore"
		failed.FinishedAt = m.now()
		if _, err := m.backups.FinishRestore(ctx, failed); err != nil {
			m.logger.Warn("databases: could not fail a stale restore",
				"restore_id", restore.ID.String(), "error", err)
			continue
		}
		m.logger.Info("databases: marked stale restore failed",
			"restore_id", restore.ID.String(), "database_id", restore.DatabaseID.String())
		database, ok := m.releaseInterrupted(ctx, restore.DatabaseID)
		if !ok {
			continue
		}
		if staged, pathErr := stagingPath(database, restore.BackupID); pathErr == nil {
			m.removeStagedArtifact(database, staged)
		}
		m.markError(ctx, database, errors.New("control plane restarted during the restore"))
	}
}

func (m *BackupManager) releaseInterrupted(ctx context.Context, databaseID uuid.UUID) (Database, bool) {
	database, err := m.repo.GetDatabase(ctx, databaseID)
	if err != nil {
		return Database{}, false
	}
	m.cleanupOrphanJobs(ctx, database)
	return database, true
}

func (m *BackupManager) resumeAfterSweep(ctx context.Context, database Database) {
	if err := m.resumeDatabase(ctx, database); err != nil {
		m.logger.Warn("databases: could not resume a database after a stale job",
			"database_id", database.ID.String(), "error", err)
	}
}

func (m *BackupManager) cleanupOrphanJobs(ctx context.Context, database Database) {
	if m.containers == nil || database.ServerID == uuid.Nil {
		return
	}
	list, err := m.containers.List(ctx, database.ServerID)
	if err != nil {
		m.logger.Warn("databases: could not list job containers for recovery",
			"database_id", database.ID.String(), "error", err)
		return
	}
	for _, item := range list {
		if !isOrphanJobContainer(item, database.ID) {
			continue
		}
		if err := m.containers.Remove(ctx, database.ServerID, item.ID); err != nil &&
			!errors.Is(err, containers.ErrContainerNotFound) {
			m.logger.Warn("databases: could not remove an orphan job container",
				"database_id", database.ID.String(), "container_id", item.ID, "error", err)
		}
	}
}

func isOrphanJobContainer(item containers.Container, databaseID uuid.UUID) bool {
	if item.Labels[labelManaged] != "true" || item.Labels[labelDatabaseID] != databaseID.String() {
		return false
	}
	switch item.Labels[labelRole] {
	case roleBackup, roleRestore, roleStage:
		return true
	default:
		return false
	}
}

func (m *BackupManager) backupsReady() error {
	if m == nil || m.backups == nil {
		return errors.New("databases: backup repository is not configured")
	}
	return nil
}

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

func (m *BackupManager) ListBackups(ctx context.Context, userID, databaseID uuid.UUID, limit int) ([]Backup, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if _, err := m.database(ctx, userID, databaseID, false); err != nil {
		return nil, err
	}
	backups, err := m.backups.ListBackupsByDatabase(ctx, databaseID, clampBackupListLimit(limit))
	if err != nil {
		return nil, err
	}
	if backups == nil {
		return []Backup{}, nil
	}
	return backups, nil
}

func (m *BackupManager) ListRestores(ctx context.Context, userID, databaseID uuid.UUID, limit int) ([]Restore, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if _, err := m.database(ctx, userID, databaseID, false); err != nil {
		return nil, err
	}
	restores, err := m.backups.ListRestoresByDatabase(ctx, databaseID, clampBackupListLimit(limit))
	if err != nil {
		return nil, err
	}
	if restores == nil {
		return []Restore{}, nil
	}
	return restores, nil
}

func clampBackupListLimit(limit int) int {
	if limit <= 0 {
		return defaultBackupListLimit
	}
	if limit > maxBackupListLimit {
		return maxBackupListLimit
	}
	return limit
}

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
	}
	if _, err := m.backups.DeleteBackup(ctx, backupID); err != nil {
		return err
	}
	return nil
}

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
	if !m.claimRestore(database.ID) {
		return RestoreResult{}, ErrBackupInFlight
	}
	restore := Restore{
		ID:         uuid.New(),
		DatabaseID: database.ID,
		BackupID:   backup.ID,
		Status:     RestoreRunning,
		CreatedAt:  m.now(),
	}
	stored, err := m.backups.CreateRestore(ctx, restore)
	if err != nil {
		m.release(database.ID)
		return RestoreResult{}, err
	}
	go m.runRestore(stored, backup, database)
	return RestoreResult{
		RestoreID:  stored.ID,
		BackupID:   backup.ID,
		DatabaseID: database.ID,
		Location:   backup.Location,
		Status:     BackupRunning,
	}, nil
}

func (m *BackupManager) databaseCredentials(ctx context.Context, database Database) (Credentials, error) {
	secrets, err := m.repo.ListSecrets(ctx, database.ID)
	if err != nil {
		return Credentials{}, err
	}
	return openCredentials(m.secret, secrets)
}

func (m *BackupManager) claim(databaseID uuid.UUID) bool {
	return m.leases.Claim(databaseID, JobLeaseBackup)
}

func (m *BackupManager) claimRun(databaseID, backupID uuid.UUID) bool {
	return m.leases.ClaimRun(databaseID, JobLeaseBackup, backupID)
}

func (m *BackupManager) claimRestore(databaseID uuid.UUID) bool {
	return m.leases.Claim(databaseID, JobLeaseRestore)
}

func (m *BackupManager) release(databaseID uuid.UUID) {
	m.leases.Release(databaseID)
}

func resolveLocalBackupDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if fromEnv := strings.TrimSpace(os.Getenv(localBackupDirEnv)); fromEnv != "" {
		return fromEnv
	}
	return defaultLocalBackupDir
}
