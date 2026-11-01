package backups

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type BackupRepository interface {
	CreateBackup(ctx context.Context, backup Backup) (Backup, error)
	CreateBackupWithTarget(ctx context.Context, backup Backup, validate func(*BackupTarget) error) (Backup, *BackupTarget, error)
	GetBackup(ctx context.Context, backupID uuid.UUID) (Backup, error)
	ListBackupsByResource(ctx context.Context, ref ResourceRef, status BackupStatus, limit, offset int) ([]Backup, error)
	ListBackups(ctx context.Context, viewerID uuid.UUID, status BackupStatus, limit, offset int) ([]Backup, error)
	ListRunningBackups(ctx context.Context) ([]Backup, error)
	FinishBackup(ctx context.Context, backup Backup) (Backup, error)
	DeleteBackup(ctx context.Context, backupID uuid.UUID) (Backup, error)
	SetBackupWasRunning(ctx context.Context, backupID uuid.UUID, wasRunning bool) error

	ListCompletedScheduledBackups(ctx context.Context, scheduleID uuid.UUID) ([]Backup, error)
	ListFailedBackupsOlderThan(ctx context.Context, before time.Time) ([]Backup, error)
	SetBackupCleanupPending(ctx context.Context, backupID uuid.UUID, next time.Time) (Backup, error)
	SetBackupCleanupError(ctx context.Context, backupID uuid.UUID, message string, attempts int, next time.Time) (Backup, error)
	ListDuePendingCleanups(ctx context.Context, now time.Time) ([]Backup, error)
	MarkBackupsProviderRemoved(ctx context.Context, targetID uuid.UUID) error

	CreateRestore(ctx context.Context, restore Restore) (Restore, error)
	FinishRestore(ctx context.Context, restore Restore) (Restore, error)
	ListRunningRestores(ctx context.Context) ([]Restore, error)
	ListRestoresByResource(ctx context.Context, ref ResourceRef, limit, offset int) ([]Restore, error)
	ListRestores(ctx context.Context, viewerID uuid.UUID, limit, offset int) ([]Restore, error)

	CreateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error)
	GetBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error)
	ListBackupSchedulesByResource(ctx context.Context, ref ResourceRef) ([]BackupSchedule, error)
	ListBackupSchedules(ctx context.Context, viewerID uuid.UUID) ([]BackupSchedule, error)
	UpdateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error)
	DeleteBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error)
	ListDueBackupSchedules(ctx context.Context, now time.Time) ([]BackupSchedule, error)
	MarkBackupScheduleRun(ctx context.Context, scheduleID uuid.UUID, lastRun, nextRun time.Time) (BackupSchedule, error)

	ListDueArtifactCleanups(ctx context.Context, now time.Time) ([]ArtifactCleanup, error)
	SetArtifactCleanupError(ctx context.Context, id uuid.UUID, message string, attempts int, next time.Time) error
	DeleteArtifactCleanup(ctx context.Context, id uuid.UUID) error
}

type storeBackupRepository struct {
	store *store.Store
}

var _ BackupRepository = (*storeBackupRepository)(nil)

func newStoreBackupRepository(st *store.Store) *storeBackupRepository {
	return &storeBackupRepository{store: st}
}

func (r *storeBackupRepository) CreateBackup(ctx context.Context, backup Backup) (Backup, error) {
	params := createBackupParams(backup)
	row, err := r.store.CreateBackup(ctx, params)
	if err != nil {
		return Backup{}, fmt.Errorf("backups: create backup: %w", err)
	}
	return backupFromRow(row), nil
}

func (r *storeBackupRepository) CreateBackupWithTarget(ctx context.Context, backup Backup, validate func(*BackupTarget) error) (Backup, *BackupTarget, error) {
	storeValidate := func(target sqlc.BackupTarget) error {
		if validate == nil {
			return nil
		}
		live := targetFromRow(target)
		return validate(&live)
	}
	row, target, err := r.store.CreateBackupWithTarget(ctx, createBackupParams(backup), storeValidate)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, nil, ErrNotFound
		}
		return Backup{}, nil, fmt.Errorf("backups: create backup: %w", err)
	}
	if target == nil {
		return backupFromRow(row), nil, nil
	}
	live := targetFromRow(*target)
	return backupFromRow(row), &live, nil
}

func (r *storeBackupRepository) GetBackup(ctx context.Context, backupID uuid.UUID) (Backup, error) {
	row, err := r.store.GetBackup(ctx, pgUUID(backupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("backups: get backup: %w", err)
	}
	return backupFromRow(row), nil
}

func (r *storeBackupRepository) ListBackupsByResource(ctx context.Context, ref ResourceRef, status BackupStatus, limit, offset int) ([]Backup, error) {
	params := sqlc.ListBackupsByResourceParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	}
	db, app, svc := resourceColumns(ref)
	params.DatabaseID, params.ApplicationID, params.ServiceID = db, app, svc
	if status != "" {
		value := string(status)
		params.Status = &value
	}
	rows, err := r.store.ListBackupsByResource(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("backups: list backups: %w", err)
	}
	return backupsFromRows(rows), nil
}

func (r *storeBackupRepository) ListBackups(ctx context.Context, viewerID uuid.UUID, status BackupStatus, limit, offset int) ([]Backup, error) {
	params := sqlc.ListBackupsParams{
		Limit:    int32(limit),
		Offset:   int32(offset),
		ViewerID: pgUUID(viewerID),
	}
	if status != "" {
		value := string(status)
		params.Status = &value
	}
	rows, err := r.store.ListBackups(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("backups: list backups: %w", err)
	}
	return backupsFromRows(rows), nil
}

func (r *storeBackupRepository) ListRunningBackups(ctx context.Context) ([]Backup, error) {
	rows, err := r.store.ListRunningBackups(ctx)
	if err != nil {
		return nil, fmt.Errorf("backups: list running backups: %w", err)
	}
	return backupsFromRows(rows), nil
}

func (r *storeBackupRepository) FinishBackup(ctx context.Context, backup Backup) (Backup, error) {
	row, err := r.store.FinishBackup(ctx, sqlc.FinishBackupParams{
		ID:             pgUUID(backup.ID),
		Status:         string(backup.Status),
		Size:           backup.Size,
		Location:       backup.Location,
		Error:          backup.Error,
		ContainerID:    backup.ContainerID,
		FinishedAt:     timeToPG(backup.FinishedAt),
		ChecksumSha256: optionalString(backup.ChecksumSHA256),
		ChecksumAlgo:   optionalString(backup.ChecksumAlgo),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("backups: finish backup: %w", err)
	}
	return backupFromRow(row), nil
}

func (r *storeBackupRepository) DeleteBackup(ctx context.Context, backupID uuid.UUID) (Backup, error) {
	row, err := r.store.DeleteBackup(ctx, pgUUID(backupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("backups: delete backup: %w", err)
	}
	return backupFromRow(row), nil
}

func (r *storeBackupRepository) SetBackupWasRunning(ctx context.Context, backupID uuid.UUID, wasRunning bool) error {
	if err := r.store.SetBackupWasRunning(ctx, sqlc.SetBackupWasRunningParams{
		ID:         pgUUID(backupID),
		WasRunning: wasRunning,
	}); err != nil {
		return fmt.Errorf("backups: record backup was_running: %w", err)
	}
	return nil
}

func (r *storeBackupRepository) ListCompletedScheduledBackups(ctx context.Context, scheduleID uuid.UUID) ([]Backup, error) {
	rows, err := r.store.ListCompletedScheduledBackups(ctx, pgUUID(scheduleID))
	if err != nil {
		return nil, fmt.Errorf("backups: list scheduled backups: %w", err)
	}
	return backupsFromRows(rows), nil
}

func (r *storeBackupRepository) ListFailedBackupsOlderThan(ctx context.Context, before time.Time) ([]Backup, error) {
	rows, err := r.store.ListFailedBackupsOlderThan(ctx, timeToPG(before))
	if err != nil {
		return nil, fmt.Errorf("backups: list failed backups: %w", err)
	}
	return backupsFromRows(rows), nil
}

func (r *storeBackupRepository) SetBackupCleanupPending(ctx context.Context, backupID uuid.UUID, next time.Time) (Backup, error) {
	row, err := r.store.SetBackupCleanupPending(ctx, sqlc.SetBackupCleanupPendingParams{
		ID:                   pgUUID(backupID),
		CleanupNextAttemptAt: timeToPG(next),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("backups: mark cleanup pending: %w", err)
	}
	return backupFromRow(row), nil
}

func (r *storeBackupRepository) SetBackupCleanupError(ctx context.Context, backupID uuid.UUID, message string, attempts int, next time.Time) (Backup, error) {
	row, err := r.store.SetBackupCleanupError(ctx, sqlc.SetBackupCleanupErrorParams{
		ID:                   pgUUID(backupID),
		CleanupError:         message,
		CleanupAttempts:      int32(attempts),
		CleanupNextAttemptAt: timeToPG(next),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("backups: record cleanup failure: %w", err)
	}
	return backupFromRow(row), nil
}

func (r *storeBackupRepository) ListDuePendingCleanups(ctx context.Context, now time.Time) ([]Backup, error) {
	rows, err := r.store.ListDuePendingCleanups(ctx, timeToPG(now))
	if err != nil {
		return nil, fmt.Errorf("backups: list pending cleanups: %w", err)
	}
	return backupsFromRows(rows), nil
}

func (r *storeBackupRepository) MarkBackupsProviderRemoved(ctx context.Context, targetID uuid.UUID) error {
	if err := r.store.MarkBackupsProviderRemoved(ctx, pgUUID(targetID)); err != nil {
		return fmt.Errorf("backups: record provider removal: %w", err)
	}
	return nil
}

func (r *storeBackupRepository) CreateRestore(ctx context.Context, restore Restore) (Restore, error) {
	db, app, svc := resourceColumns(restore.ResourceRef)
	row, err := r.store.CreateRestore(ctx, sqlc.CreateRestoreParams{
		ID:            pgUUID(restore.ID),
		DatabaseID:    db,
		ApplicationID: app,
		ServiceID:     svc,
		BackupID:      pgUUID(restore.BackupID),
		Status:        string(restore.Status),
	})
	if err != nil {
		return Restore{}, fmt.Errorf("backups: create restore: %w", err)
	}
	return restoreFromRow(row), nil
}

func (r *storeBackupRepository) FinishRestore(ctx context.Context, restore Restore) (Restore, error) {
	row, err := r.store.FinishRestore(ctx, sqlc.FinishRestoreParams{
		ID:         pgUUID(restore.ID),
		Status:     string(restore.Status),
		Error:      restore.Error,
		FinishedAt: timeToPG(restore.FinishedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Restore{}, ErrNotFound
		}
		return Restore{}, fmt.Errorf("backups: finish restore: %w", err)
	}
	return restoreFromRow(row), nil
}

func (r *storeBackupRepository) ListRunningRestores(ctx context.Context) ([]Restore, error) {
	rows, err := r.store.ListRunningRestores(ctx)
	if err != nil {
		return nil, fmt.Errorf("backups: list running restores: %w", err)
	}
	return restoresFromRows(rows), nil
}

func (r *storeBackupRepository) ListRestoresByResource(ctx context.Context, ref ResourceRef, limit, offset int) ([]Restore, error) {
	db, app, svc := resourceColumns(ref)
	rows, err := r.store.ListRestoresByResource(ctx, sqlc.ListRestoresByResourceParams{
		Limit:         int32(limit),
		Offset:        int32(offset),
		DatabaseID:    db,
		ApplicationID: app,
		ServiceID:     svc,
	})
	if err != nil {
		return nil, fmt.Errorf("backups: list restores: %w", err)
	}
	return restoresFromRows(rows), nil
}

func (r *storeBackupRepository) ListRestores(ctx context.Context, viewerID uuid.UUID, limit, offset int) ([]Restore, error) {
	rows, err := r.store.ListRestores(ctx, sqlc.ListRestoresParams{
		Limit:    int32(limit),
		Offset:   int32(offset),
		ViewerID: pgUUID(viewerID),
	})
	if err != nil {
		return nil, fmt.Errorf("backups: list restores: %w", err)
	}
	return restoresFromRows(rows), nil
}

func (r *storeBackupRepository) CreateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error) {
	maxRunSeconds := schedule.MaxRunSeconds
	if maxRunSeconds <= 0 {
		maxRunSeconds = DefaultMaxBackupRunSeconds
	}
	db, app, svc := resourceColumns(schedule.ResourceRef)
	row, err := r.store.CreateBackupSchedule(ctx, sqlc.CreateBackupScheduleParams{
		ID:                pgUUID(schedule.ID),
		DatabaseID:        db,
		ApplicationID:     app,
		ServiceID:         svc,
		Cron:              schedule.Cron,
		TargetID:          pgUUID(schedule.TargetID),
		Enabled:           schedule.Enabled,
		NextRunAt:         timeToPG(schedule.NextRunAt),
		MaxRunSeconds:     int32(maxRunSeconds),
		PauseDuringBackup: schedule.PauseDuringBackup,
		KeepLast:          keepLastParam(schedule.KeepLast),
	})
	if err != nil {
		return BackupSchedule{}, fmt.Errorf("backups: create backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

func (r *storeBackupRepository) GetBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	row, err := r.store.GetBackupSchedule(ctx, pgUUID(scheduleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("backups: get backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

func (r *storeBackupRepository) ListBackupSchedulesByResource(ctx context.Context, ref ResourceRef) ([]BackupSchedule, error) {
	db, app, svc := resourceColumns(ref)
	rows, err := r.store.ListBackupSchedulesByResource(ctx, sqlc.ListBackupSchedulesByResourceParams{
		DatabaseID:    db,
		ApplicationID: app,
		ServiceID:     svc,
	})
	if err != nil {
		return nil, fmt.Errorf("backups: list backup schedules: %w", err)
	}
	schedules := make([]BackupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row))
	}
	return schedules, nil
}

func (r *storeBackupRepository) ListBackupSchedules(ctx context.Context, viewerID uuid.UUID) ([]BackupSchedule, error) {
	rows, err := r.store.ListBackupSchedules(ctx, pgUUID(viewerID))
	if err != nil {
		return nil, fmt.Errorf("backups: list backup schedules: %w", err)
	}
	schedules := make([]BackupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row))
	}
	return schedules, nil
}

func (r *storeBackupRepository) UpdateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error) {
	maxRunSeconds := schedule.MaxRunSeconds
	if maxRunSeconds <= 0 {
		maxRunSeconds = DefaultMaxBackupRunSeconds
	}
	row, err := r.store.UpdateBackupSchedule(ctx, sqlc.UpdateBackupScheduleParams{
		ID:                pgUUID(schedule.ID),
		Cron:              schedule.Cron,
		TargetID:          pgUUID(schedule.TargetID),
		Enabled:           schedule.Enabled,
		NextRunAt:         timeToPG(schedule.NextRunAt),
		LastSlotAt:        timeToPG(schedule.LastSlotAt),
		TargetChangedAt:   timeToPG(schedule.TargetChangedAt),
		MaxRunSeconds:     int32(maxRunSeconds),
		PauseDuringBackup: schedule.PauseDuringBackup,
		KeepLast:          keepLastParam(schedule.KeepLast),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("backups: update backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

func (r *storeBackupRepository) DeleteBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	row, err := r.store.DeleteBackupSchedule(ctx, pgUUID(scheduleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("backups: delete backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

func (r *storeBackupRepository) ListDueBackupSchedules(ctx context.Context, now time.Time) ([]BackupSchedule, error) {
	rows, err := r.store.ListDueBackupSchedules(ctx, timeToPG(now.UTC()))
	if err != nil {
		return nil, fmt.Errorf("backups: list due backup schedules: %w", err)
	}
	schedules := make([]BackupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row))
	}
	return schedules, nil
}

func (r *storeBackupRepository) MarkBackupScheduleRun(ctx context.Context, scheduleID uuid.UUID, lastRun, nextRun time.Time) (BackupSchedule, error) {
	row, err := r.store.MarkBackupScheduleRun(ctx, sqlc.MarkBackupScheduleRunParams{
		ID:        pgUUID(scheduleID),
		LastRunAt: timeToPG(lastRun),
		NextRunAt: timeToPG(nextRun),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("backups: mark backup schedule run: %w", err)
	}
	return scheduleFromRow(row), nil
}

func (r *storeBackupRepository) ListDueArtifactCleanups(ctx context.Context, now time.Time) ([]ArtifactCleanup, error) {
	rows, err := r.store.ListDueArtifactCleanups(ctx, timeToPG(now))
	if err != nil {
		return nil, fmt.Errorf("backups: list pending artifact cleanups: %w", err)
	}
	cleanups := make([]ArtifactCleanup, 0, len(rows))
	for _, row := range rows {
		cleanups = append(cleanups, artifactCleanupFromRow(row))
	}
	return cleanups, nil
}

func (r *storeBackupRepository) SetArtifactCleanupError(ctx context.Context, id uuid.UUID, message string, attempts int, next time.Time) error {
	if err := r.store.SetArtifactCleanupError(ctx, sqlc.SetArtifactCleanupErrorParams{
		ID:            pgUUID(id),
		Attempts:      int32(attempts),
		NextAttemptAt: timeToPG(next),
		LastError:     message,
	}); err != nil {
		return fmt.Errorf("backups: record artifact cleanup failure: %w", err)
	}
	return nil
}

func (r *storeBackupRepository) DeleteArtifactCleanup(ctx context.Context, id uuid.UUID) error {
	if err := r.store.DeleteArtifactCleanup(ctx, pgUUID(id)); err != nil {
		return fmt.Errorf("backups: delete artifact cleanup: %w", err)
	}
	return nil
}

func (r *storeBackupRepository) ListServerIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.store.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("backups: list servers: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, uuidFromPG(row.ID))
	}
	return ids, nil
}

func (r *storeBackupRepository) ListOwnerIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.store.ListOwnerUserIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("backups: list owner accounts: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, uuidFromPG(row))
	}
	return ids, nil
}

func createBackupParams(backup Backup) sqlc.CreateBackupParams {
	db, app, svc := resourceColumns(backup.ResourceRef)
	return sqlc.CreateBackupParams{
		ID:                pgUUID(backup.ID),
		DatabaseID:        db,
		ApplicationID:     app,
		ServiceID:         svc,
		ScheduleID:        pgUUID(backup.ScheduleID),
		Type:              string(backup.Type),
		Status:            string(backup.Status),
		Size:              backup.Size,
		Location:          backup.Location,
		TargetID:          pgUUID(backup.TargetID),
		ContainerID:       backup.ContainerID,
		Error:             backup.Error,
		FinishedAt:        timeToPG(backup.FinishedAt),
		PauseDuringBackup: backup.PauseDuringBackup,
	}
}

func backupsFromRows(rows []sqlc.Backup) []Backup {
	backups := make([]Backup, 0, len(rows))
	for _, row := range rows {
		backups = append(backups, backupFromRow(row))
	}
	return backups
}

func restoresFromRows(rows []sqlc.Restore) []Restore {
	restores := make([]Restore, 0, len(rows))
	for _, row := range rows {
		restores = append(restores, restoreFromRow(row))
	}
	return restores
}

func backupFromRow(row sqlc.Backup) Backup {
	return Backup{
		ID:                   uuidFromPG(row.ID),
		ResourceRef:          resourceRef(row.DatabaseID, row.ApplicationID, row.ServiceID),
		ScheduleID:           uuidFromPG(row.ScheduleID),
		Type:                 BackupType(row.Type),
		Status:               BackupStatus(row.Status),
		Size:                 row.Size,
		Location:             row.Location,
		TargetID:             uuidFromPG(row.TargetID),
		ContainerID:          row.ContainerID,
		WasRunning:           row.WasRunning,
		PauseDuringBackup:    row.PauseDuringBackup,
		ChecksumSHA256:       stringFromPG(row.ChecksumSha256),
		ChecksumAlgo:         stringFromPG(row.ChecksumAlgo),
		Error:                row.Error,
		ProviderRemoved:      row.ProviderRemoved,
		CleanupPending:       row.CleanupPending,
		CleanupError:         row.CleanupError,
		CleanupAttempts:      int(row.CleanupAttempts),
		CleanupNextAttemptAt: timeFromPG(row.CleanupNextAttemptAt),
		CreatedAt:            timeFromPG(row.CreatedAt),
		FinishedAt:           timeFromPG(row.FinishedAt),
	}
}

func artifactCleanupFromRow(row sqlc.BackupArtifactCleanup) ArtifactCleanup {
	return ArtifactCleanup{
		ID:            uuidFromPG(row.ID),
		TargetID:      uuidFromPG(row.TargetID),
		Location:      row.Location,
		Attempts:      int(row.Attempts),
		NextAttemptAt: timeFromPG(row.NextAttemptAt),
		LastError:     row.LastError,
		CreatedAt:     timeFromPG(row.CreatedAt),
	}
}

func keepLastParam(keep *int) *int32 {
	if keep == nil {
		return nil
	}
	value := int32(*keep)
	return &value
}

func restoreFromRow(row sqlc.Restore) Restore {
	return Restore{
		ID:          uuidFromPG(row.ID),
		ResourceRef: resourceRef(row.DatabaseID, row.ApplicationID, row.ServiceID),
		BackupID:    uuidFromPG(row.BackupID),
		Status:      RestoreStatus(row.Status),
		Error:       row.Error,
		CreatedAt:   timeFromPG(row.CreatedAt),
		FinishedAt:  timeFromPG(row.FinishedAt),
	}
}

func scheduleFromRow(row sqlc.BackupSchedule) BackupSchedule {
	return BackupSchedule{
		ID:                uuidFromPG(row.ID),
		ResourceRef:       resourceRef(row.DatabaseID, row.ApplicationID, row.ServiceID),
		Cron:              row.Cron,
		TargetID:          uuidFromPG(row.TargetID),
		Enabled:           row.Enabled,
		LastRunAt:         timeFromPG(row.LastRunAt),
		NextRunAt:         timeFromPG(row.NextRunAt),
		LastSlotAt:        timeFromPG(row.LastSlotAt),
		TargetChangedAt:   timeFromPG(row.TargetChangedAt),
		MaxRunSeconds:     int(row.MaxRunSeconds),
		PauseDuringBackup: row.PauseDuringBackup,
		KeepLast:          intPtrFromPG(row.KeepLast),
		CreatedAt:         timeFromPG(row.CreatedAt),
		UpdatedAt:         timeFromPG(row.UpdatedAt),
	}
}

func intPtrFromPG(value *int32) *int {
	if value == nil {
		return nil
	}
	parsed := int(*value)
	return &parsed
}

func targetFromRow(row sqlc.BackupTarget) BackupTarget {
	return BackupTarget{
		ID:        uuidFromPG(row.ID),
		UserID:    uuidFromPG(row.UserID),
		Name:      row.Name,
		Kind:      TargetKind(row.Kind),
		Endpoint:  row.Endpoint,
		Region:    row.Region,
		Bucket:    row.Bucket,
		Prefix:    row.Prefix,
		Builtin:   row.Builtin,
		CreatedAt: timeFromPG(row.CreatedAt),
		UpdatedAt: timeFromPG(row.UpdatedAt),
	}
}

// exactly one of the three columns set (CHECK constraint).
func resourceRef(database, application, service pgtype.UUID) ResourceRef {
	switch {
	case application.Valid:
		return ResourceRef{Type: ResourceApplication, ID: uuidFromPG(application)}
	case service.Valid:
		return ResourceRef{Type: ResourceService, ID: uuidFromPG(service)}
	default:
		return ResourceRef{Type: ResourceDatabase, ID: uuidFromPG(database)}
	}
}

func resourceColumns(ref ResourceRef) (database, application, service pgtype.UUID) {
	switch ref.Type {
	case ResourceApplication:
		return pgtype.UUID{}, pgUUID(ref.ID), pgtype.UUID{}
	case ResourceService:
		return pgtype.UUID{}, pgtype.UUID{}, pgUUID(ref.ID)
	default:
		return pgUUID(ref.ID), pgtype.UUID{}, pgtype.UUID{}
	}
}

func timeToPG(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
