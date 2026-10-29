package backups

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

	// featureEnv gates the whole feature; it is the databases switch today
	// because the only driver is the database one.
	featureEnv = "FEATURE_DATABASES"

	// DefaultMaxBackupRunSeconds is the documented fallback when a running
	// backup has no live control-plane lease. Owners can tune it per schedule
	// between 15 minutes and 24 hours.
	DefaultMaxBackupRunSeconds = 2 * 60 * 60
	MinMaxBackupRunSeconds     = 15 * 60
	MaxMaxBackupRunSeconds     = 24 * 60 * 60
)

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(featureEnv)), "false")
}

type BackupConfig struct {
	Store             *store.Store
	Repository        BackupRepository
	Sources           map[ResourceType]Source
	Secret            string
	Logger            *slog.Logger
	LocalDir          string
	ObjectStore       ObjectStore
	Containers        containers.ContainerService
	JobTimeout        time.Duration
	SchedulerInterval time.Duration
	DisableScheduler  bool
	Notifier          BackupNotifier
	Leases            *JobLeases
}

type BackupService interface {
	CreateBackup(ctx context.Context, userID uuid.UUID, req CreateBackupRequest) (Backup, error)
	ListBackups(ctx context.Context, userID uuid.UUID, filter BackupFilter) ([]Backup, error)
	GetBackup(ctx context.Context, userID, backupID uuid.UUID) (Backup, error)
	DeleteBackup(ctx context.Context, userID, backupID uuid.UUID) error
	RestoreBackup(ctx context.Context, userID, backupID uuid.UUID) (RestoreResult, error)
	ListRestores(ctx context.Context, userID uuid.UUID, filter RestoreFilter) ([]Restore, error)
	Contents(ctx context.Context, userID uuid.UUID, ref ResourceRef) ([]Content, error)

	ListSchedules(ctx context.Context, userID uuid.UUID, ref ResourceRef) ([]BackupSchedule, error)
	CreateSchedule(ctx context.Context, userID uuid.UUID, req ScheduleRequest) (BackupSchedule, error)
	UpdateSchedule(ctx context.Context, userID, scheduleID uuid.UUID, req ScheduleRequest) (BackupSchedule, error)
	DeleteSchedule(ctx context.Context, userID, scheduleID uuid.UUID) error

	ListProviders(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error)
	CreateProvider(ctx context.Context, userID uuid.UUID, req ProviderRequest) (BackupTarget, error)
	UpdateProvider(ctx context.Context, userID, providerID uuid.UUID, req ProviderRequest) (BackupTarget, error)
	DeleteProvider(ctx context.Context, userID, providerID uuid.UUID) error
	TestProvider(ctx context.Context, userID, providerID uuid.UUID) (TargetCheck, error)

	Close() error
}

type CreateBackupRequest struct {
	Resource          ResourceRef
	TargetID          uuid.UUID
	ScheduleID        uuid.UUID
	PauseDuringBackup *bool
}

type BackupFilter struct {
	Resource *ResourceRef
	Status   BackupStatus
	Limit    int
	Offset   int
}

type RestoreFilter struct {
	Resource *ResourceRef
	Limit    int
	Offset   int
}

type RestoreResult struct {
	RestoreID   uuid.UUID    `json:"restore_id"`
	BackupID    uuid.UUID    `json:"backup_id"`
	ResourceRef ResourceRef  `json:"resource"`
	Location    string       `json:"location"`
	Status      BackupStatus `json:"status"`
}

type ScheduleRequest struct {
	Resource          ResourceRef `json:"resource"`
	Cron              string      `json:"cron"`
	TargetID          *string     `json:"provider_id,omitempty"`
	Enabled           *bool       `json:"enabled,omitempty"`
	MaxRunSeconds     *int        `json:"max_run_seconds,omitempty"`
	PauseDuringBackup *bool       `json:"pause_during_backup,omitempty"`
}

type ProviderRequest struct {
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
	backups      BackupRepository
	objects      ObjectStore
	sources      map[ResourceType]Source
	containers   containers.ContainerService
	secret       string
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

	leases := cfg.Leases
	if leases == nil {
		leases = NewJobLeases()
	}
	sources := make(map[ResourceType]Source, len(cfg.Sources))
	for kind, source := range cfg.Sources {
		if source != nil {
			sources[kind] = source
		}
	}
	manager := &BackupManager{
		backups:      backupRepo,
		objects:      cfg.ObjectStore,
		sources:      sources,
		containers:   cfg.Containers,
		secret:       cfg.Secret,
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
	if len(cfg.Sources) == 0 {
		return nil
	}
	if !Enabled() {
		return nil
	}
	manager := NewBackupService(cfg)
	manager.ensureBuiltinProviders()
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

// JobActive reports whether this control plane holds any backup or restore
// lease for the resource, so a lifecycle action can be refused instead of
// racing the job that is reading the same data.
func (m *BackupManager) JobActive(resourceID uuid.UUID) bool {
	return m != nil && m.leases.Held(resourceID) != ""
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

func (m *BackupManager) source(kind ResourceType) (Source, error) {
	source, ok := m.sources[kind]
	if !ok || source == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedResource, kind)
	}
	return source, nil
}

func (m *BackupManager) resolve(ctx context.Context, ref ResourceRef) (Resource, Source, error) {
	if !ref.Valid() {
		return Resource{}, nil, fmt.Errorf("%w: resource_type and resource_id are required", ErrValidation)
	}
	source, err := m.source(ref.Type)
	if err != nil {
		return Resource{}, nil, err
	}
	resource, err := source.Resolve(ctx, ref)
	if err != nil {
		return Resource{}, nil, err
	}
	return resource, source, nil
}

// ensureBuiltinProviders creates the built-in Local disk provider for every
// owner account, so a fresh install can back up with no setup.
func (m *BackupManager) ensureBuiltinProviders() {
	lister, ok := m.backups.(interface {
		ListOwnerIDs(context.Context) ([]uuid.UUID, error)
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids, err := lister.ListOwnerIDs(ctx)
	if err != nil {
		m.logger.Warn("backups: could not list owner accounts", "error", err)
		return
	}
	for _, id := range ids {
		if _, err := m.backups.EnsureBuiltinTarget(ctx, id); err != nil {
			m.logger.Warn("backups: could not create the built-in provider",
				"user_id", id.String(), "error", err)
		}
	}
}

func (m *BackupManager) reconcileStaleBackups() {
	if m.backups == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stale, backupErr := m.backups.ListRunningBackups(ctx)
	if backupErr != nil {
		m.logger.Warn("backups: stale backup sweep failed", "error", backupErr)
	}
	restores, restoreErr := m.backups.ListRunningRestores(ctx)
	if restoreErr != nil {
		m.logger.Warn("backups: stale restore sweep failed", "error", restoreErr)
	}
	restoring := make(map[uuid.UUID]bool, len(restores))
	for _, restore := range restores {
		restoring[restore.ResourceRef.ID] = true
	}

	if backupErr == nil {
		for _, backup := range stale {
			if kind := m.leases.Held(backup.ResourceRef.ID); kind != "" {
				m.logger.Warn("backups: not sweeping a backup with a live job",
					"backup_id", backup.ID.String(), "lease", string(kind))
				continue
			}
			failed := backup
			failed.Status = BackupFailed
			failed.Error = "control plane restarted during the backup"
			failed.FinishedAt = m.now()
			if _, err := m.backups.FinishBackup(ctx, failed); err != nil {
				m.logger.Warn("backups: could not fail a stale backup",
					"backup_id", backup.ID.String(), "error", err)
				continue
			}
			m.logger.Info("backups: marked stale backup failed",
				"backup_id", backup.ID.String(), "resource", backup.ResourceRef.String())
			m.recover(ctx, backup.ResourceRef, backup.ID, false, backup.WasRunning && !restoring[backup.ResourceRef.ID])
		}
	}

	if restoreErr != nil {
		return
	}
	for _, restore := range restores {
		if kind := m.leases.Held(restore.ResourceRef.ID); kind != "" {
			m.logger.Warn("backups: not sweeping a restore with a live job",
				"restore_id", restore.ID.String(), "lease", string(kind))
			continue
		}
		failed := restore
		failed.Status = RestoreFailed
		failed.Error = "control plane restarted during the restore"
		failed.FinishedAt = m.now()
		if _, err := m.backups.FinishRestore(ctx, failed); err != nil {
			m.logger.Warn("backups: could not fail a stale restore",
				"restore_id", restore.ID.String(), "error", err)
			continue
		}
		m.logger.Info("backups: marked stale restore failed",
			"restore_id", restore.ID.String(), "resource", restore.ResourceRef.String())
		m.recover(ctx, restore.ResourceRef, restore.BackupID, true, false)
	}
}

// recover hands a resource left behind by an interrupted run back to its
// driver. A deleted resource simply has nothing to recover.
func (m *BackupManager) recover(ctx context.Context, ref ResourceRef, backupID uuid.UUID, wasRestore, wasRunning bool) {
	source, err := m.source(ref.Type)
	if err != nil {
		return
	}
	resource, err := source.Resolve(ctx, ref)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			m.logger.Warn("backups: could not resolve a resource after an interrupted run",
				"resource", ref.String(), "error", err)
		}
		return
	}
	if err := source.Recover(ctx, RecoverRequest{
		Resource:   resource,
		BackupID:   backupID,
		WasRestore: wasRestore,
		WasRunning: wasRunning,
	}); err != nil {
		m.logger.Warn("backups: could not recover an interrupted run",
			"resource", ref.String(), "error", err)
	}
}

func (m *BackupManager) backupsReady() error {
	if m == nil || m.backups == nil {
		return errors.New("backups: backup repository is not configured")
	}
	return nil
}

func (m *BackupManager) CreateBackup(ctx context.Context, userID uuid.UUID, req CreateBackupRequest) (Backup, error) {
	if !Enabled() {
		return Backup{}, ErrDisabled
	}
	resource, source, err := m.resolve(ctx, req.Resource)
	if err != nil {
		return Backup{}, err
	}
	if validator, ok := source.(Validator); ok {
		if err := validator.Validate(ctx, resource); err != nil {
			return Backup{}, err
		}
	}
	target, err := m.ownedTarget(ctx, userID, req.TargetID)
	if err != nil {
		return Backup{}, err
	}
	if target == nil {
		if target, err = m.defaultTarget(ctx, resource.OwnerID); err != nil {
			return Backup{}, err
		}
	}
	pause := true
	if req.PauseDuringBackup != nil {
		pause = *req.PauseDuringBackup
	}
	return m.startRun(ctx, source, resource, target, BackupManual, req.ScheduleID, pause)
}

// Contents lists what a resource's next run captures. A source without a
// driver-specific list answers with nothing.
func (m *BackupManager) Contents(ctx context.Context, userID uuid.UUID, ref ResourceRef) ([]Content, error) {
	resource, source, err := m.resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	lister, ok := source.(ContentLister)
	if !ok {
		return []Content{}, nil
	}
	contents, err := lister.Contents(ctx, resource)
	if err != nil {
		return nil, err
	}
	if contents == nil {
		return []Content{}, nil
	}
	return contents, nil
}

func (m *BackupManager) ListBackups(ctx context.Context, userID uuid.UUID, filter BackupFilter) ([]Backup, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	limit := clampBackupListLimit(filter.Limit)
	offset := clampBackupListOffset(filter.Offset)
	if kind := BackupStatus(strings.TrimSpace(string(filter.Status))); kind != "" {
		switch kind {
		case BackupRunning, BackupCompleted, BackupFailed:
			filter.Status = kind
		default:
			return nil, fmt.Errorf("%w: status must be running, completed or failed", ErrValidation)
		}
	}
	var backups []Backup
	var err error
	if filter.Resource != nil {
		resource, _, resolveErr := m.resolve(ctx, *filter.Resource)
		if resolveErr != nil {
			return nil, resolveErr
		}
		backups, err = m.backups.ListBackupsByResource(ctx, resource.Ref(), filter.Status, limit, offset)
	} else {
		backups, err = m.backups.ListBackups(ctx, userID, filter.Status, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	if backups == nil {
		return []Backup{}, nil
	}
	m.attachProviderNames(ctx, backups)
	return backups, nil
}

// attachProviderNames fills in the display name without failing the listing
// when a provider row is gone.
func (m *BackupManager) attachProviderNames(ctx context.Context, list []Backup) {
	for i := range list {
		if list[i].TargetID == uuid.Nil {
			continue
		}
		target, err := m.backups.GetBackupTarget(ctx, list[i].TargetID)
		if err != nil {
			continue
		}
		list[i].ProviderName = target.Name
	}
}

func (m *BackupManager) ListRestores(ctx context.Context, userID uuid.UUID, filter RestoreFilter) ([]Restore, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	limit := clampBackupListLimit(filter.Limit)
	offset := clampBackupListOffset(filter.Offset)
	var restores []Restore
	var err error
	if filter.Resource != nil {
		resource, _, resolveErr := m.resolve(ctx, *filter.Resource)
		if resolveErr != nil {
			return nil, resolveErr
		}
		restores, err = m.backups.ListRestoresByResource(ctx, resource.Ref(), limit, offset)
	} else {
		restores, err = m.backups.ListRestores(ctx, userID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	if restores == nil {
		return []Restore{}, nil
	}
	return restores, nil
}

func clampBackupListOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
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

func (m *BackupManager) GetBackup(ctx context.Context, userID, backupID uuid.UUID) (Backup, error) {
	if err := m.backupsReady(); err != nil {
		return Backup{}, err
	}
	backup, err := m.backups.GetBackup(ctx, backupID)
	if err != nil {
		return Backup{}, err
	}
	if _, _, err := m.resolve(ctx, backup.ResourceRef); err != nil {
		return Backup{}, err
	}
	backup.ProviderName = m.nameForProvider(ctx, backup.TargetID)
	return backup, nil
}

func (m *BackupManager) nameForProvider(ctx context.Context, targetID uuid.UUID) string {
	if targetID == uuid.Nil {
		return ""
	}
	target, err := m.backups.GetBackupTarget(ctx, targetID)
	if err != nil {
		return ""
	}
	return target.Name
}

func (m *BackupManager) DeleteBackup(ctx context.Context, userID, backupID uuid.UUID) error {
	if err := m.backupsReady(); err != nil {
		return err
	}
	backup, err := m.backups.GetBackup(ctx, backupID)
	if err != nil {
		return err
	}
	resource, _, err := m.resolve(ctx, backup.ResourceRef)
	if err != nil {
		return err
	}
	if backup.Status == BackupRunning {
		return fmt.Errorf("%w: backup is still running", ErrBackupInFlight)
	}
	if backup.Location != "" {
		store, err := m.storeForLocation(ctx, backup, resource.OwnerID)
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

func (m *BackupManager) RestoreBackup(ctx context.Context, userID, backupID uuid.UUID) (RestoreResult, error) {
	if !Enabled() {
		return RestoreResult{}, ErrDisabled
	}
	if backupID == uuid.Nil {
		return RestoreResult{}, fmt.Errorf("%w: backup_id is required", ErrValidation)
	}
	backup, err := m.backups.GetBackup(ctx, backupID)
	if err != nil {
		return RestoreResult{}, err
	}
	resource, source, err := m.resolve(ctx, backup.ResourceRef)
	if err != nil {
		return RestoreResult{}, err
	}
	if backup.Status != BackupCompleted {
		return RestoreResult{}, fmt.Errorf("%w: only a completed backup can be restored", ErrBackupNotCompleted)
	}
	if validator, ok := source.(Validator); ok {
		if err := validator.Validate(ctx, resource); err != nil {
			return RestoreResult{}, err
		}
	}
	if !m.claimRestore(resource.ID) {
		return RestoreResult{}, ErrBackupInFlight
	}
	restore := Restore{
		ID:          uuid.New(),
		ResourceRef: resource.Ref(),
		BackupID:    backup.ID,
		Status:      RestoreRunning,
		CreatedAt:   m.now(),
	}
	stored, err := m.backups.CreateRestore(ctx, restore)
	if err != nil {
		m.release(resource.ID)
		return RestoreResult{}, err
	}
	go m.runRestore(stored, backup, resource, source)
	return RestoreResult{
		RestoreID:   stored.ID,
		BackupID:    backup.ID,
		ResourceRef: resource.Ref(),
		Location:    backup.Location,
		Status:      BackupRunning,
	}, nil
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
