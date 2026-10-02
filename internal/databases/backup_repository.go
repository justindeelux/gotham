package databases

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

// BackupRepository persists backup runs, schedules, storage targets and the
// targets' sealed credentials. It is implemented over *store.Store (sqlc) in
// production and by fakes in tests.
//
// It is a separate seam from Repository on purpose: the backup surface grows
// its own tables and its own lifecycle, and keeping the interfaces apart
// means the BE-5.1 fakes stay untouched.
type BackupRepository interface {
	// CreateBackup stores a new run, always in the running state.
	CreateBackup(ctx context.Context, backup Backup) (Backup, error)
	// GetBackup returns a run joined to a live database, or ErrNotFound.
	GetBackup(ctx context.Context, backupID uuid.UUID) (Backup, error)
	// ListBackupsByDatabase returns a database's runs, newest first.
	ListBackupsByDatabase(ctx context.Context, databaseID uuid.UUID) ([]Backup, error)
	// ListRunningBackups returns every run still marked running, oldest
	// first: the boot-time reconciliation sweep marks them failed.
	ListRunningBackups(ctx context.Context) ([]Backup, error)
	// FinishBackup persists the terminal state of a run.
	FinishBackup(ctx context.Context, backup Backup) (Backup, error)
	// DeleteBackup removes one run and returns the deleted row.
	DeleteBackup(ctx context.Context, backupID uuid.UUID) (Backup, error)
	// SetBackupWasRunning records whether the database was running when the
	// dump paused it, so the boot-time sweep can restore the pre-job state.
	SetBackupWasRunning(ctx context.Context, backupID uuid.UUID, wasRunning bool) error

	// CreateRestore stores a new restore run, always in the running state.
	CreateRestore(ctx context.Context, restore Restore) (Restore, error)
	// FinishRestore persists the terminal state of a restore run.
	FinishRestore(ctx context.Context, restore Restore) (Restore, error)
	// ListRunningRestores returns every restore still marked running, oldest
	// first: the boot-time reconciliation sweep marks them failed.
	ListRunningRestores(ctx context.Context) ([]Restore, error)

	// CreateBackupSchedule stores a schedule with its computed next run.
	CreateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error)
	// GetBackupSchedule returns a schedule of a live database.
	GetBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error)
	// ListBackupSchedulesByDatabase returns a database's schedules, newest
	// first.
	ListBackupSchedulesByDatabase(ctx context.Context, databaseID uuid.UUID) ([]BackupSchedule, error)
	// UpdateBackupSchedule persists the mutable fields (cron, target,
	// enabled, next run).
	UpdateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error)
	// DeleteBackupSchedule removes a schedule and returns the deleted row.
	DeleteBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error)
	// ListDueBackupSchedules returns enabled schedules whose next_run_at has
	// reached now, oldest first — the scheduler's single query.
	ListDueBackupSchedules(ctx context.Context, now time.Time) ([]BackupSchedule, error)
	// MarkBackupScheduleRun records a completed tick and the next due time.
	MarkBackupScheduleRun(ctx context.Context, scheduleID uuid.UUID, lastRun, nextRun time.Time) (BackupSchedule, error)

	// CreateBackupTarget stores a storage target (credentials are separate
	// sealed rows).
	CreateBackupTarget(ctx context.Context, target BackupTarget) (BackupTarget, error)
	// GetBackupTarget returns one target.
	GetBackupTarget(ctx context.Context, targetID uuid.UUID) (BackupTarget, error)
	// ListBackupTargetsByUser returns a user's targets, newest first.
	ListBackupTargetsByUser(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error)
	// UpdateBackupTarget persists the mutable configuration.
	UpdateBackupTarget(ctx context.Context, target BackupTarget) (BackupTarget, error)
	// DeleteBackupTarget removes a target owned by userID, or ErrNotFound.
	DeleteBackupTarget(ctx context.Context, targetID, userID uuid.UUID) (BackupTarget, error)

	// CreateTargetSecret stores one sealed credential of a target.
	CreateTargetSecret(ctx context.Context, secret TargetSecret) (TargetSecret, error)
	// UpsertTargetSecret stores (or replaces) one sealed credential of a
	// target, which is how a rotated access key is written.
	UpsertTargetSecret(ctx context.Context, secret TargetSecret) (TargetSecret, error)
	// ListTargetSecrets returns a target's sealed credentials, sorted by key.
	ListTargetSecrets(ctx context.Context, targetID uuid.UUID) ([]TargetSecret, error)
}

// storeBackupRepository adapts *store.Store to BackupRepository.
type storeBackupRepository struct {
	store *store.Store
}

// Compile-time guarantee.
var _ BackupRepository = (*storeBackupRepository)(nil)

// newStoreBackupRepository builds the PostgreSQL-backed repository.
func newStoreBackupRepository(st *store.Store) *storeBackupRepository {
	return &storeBackupRepository{store: st}
}

// CreateBackup implements BackupRepository.
func (r *storeBackupRepository) CreateBackup(ctx context.Context, backup Backup) (Backup, error) {
	row, err := r.store.CreateBackup(ctx, sqlc.CreateBackupParams{
		ID:          pgUUID(backup.ID),
		DatabaseID:  pgUUID(backup.DatabaseID),
		ScheduleID:  pgUUID(backup.ScheduleID),
		Type:        string(backup.Type),
		Status:      string(backup.Status),
		Size:        backup.Size,
		Location:    backup.Location,
		TargetID:    pgUUID(backup.TargetID),
		ContainerID: backup.ContainerID,
		Error:       backup.Error,
		FinishedAt:  timeToPG(backup.FinishedAt),
	})
	if err != nil {
		return Backup{}, fmt.Errorf("databases: create backup: %w", err)
	}
	return backupFromRow(row), nil
}

// GetBackup implements BackupRepository, mapping a missing row to ErrNotFound.
func (r *storeBackupRepository) GetBackup(ctx context.Context, backupID uuid.UUID) (Backup, error) {
	row, err := r.store.GetBackup(ctx, pgUUID(backupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("databases: get backup: %w", err)
	}
	return backupFromRow(row), nil
}

// ListBackupsByDatabase implements BackupRepository.
func (r *storeBackupRepository) ListBackupsByDatabase(ctx context.Context, databaseID uuid.UUID) ([]Backup, error) {
	rows, err := r.store.ListBackupsByDatabase(ctx, pgUUID(databaseID))
	if err != nil {
		return nil, fmt.Errorf("databases: list backups: %w", err)
	}
	backups := make([]Backup, 0, len(rows))
	for _, row := range rows {
		backups = append(backups, backupFromRow(row))
	}
	return backups, nil
}

// ListRunningBackups implements BackupRepository.
func (r *storeBackupRepository) ListRunningBackups(ctx context.Context) ([]Backup, error) {
	rows, err := r.store.ListRunningBackups(ctx)
	if err != nil {
		return nil, fmt.Errorf("databases: list running backups: %w", err)
	}
	backups := make([]Backup, 0, len(rows))
	for _, row := range rows {
		backups = append(backups, backupFromRow(row))
	}
	return backups, nil
}

// FinishBackup implements BackupRepository.
func (r *storeBackupRepository) FinishBackup(ctx context.Context, backup Backup) (Backup, error) {
	row, err := r.store.FinishBackup(ctx, sqlc.FinishBackupParams{
		ID:          pgUUID(backup.ID),
		Status:      string(backup.Status),
		Size:        backup.Size,
		Location:    backup.Location,
		Error:       backup.Error,
		ContainerID: backup.ContainerID,
		FinishedAt:  timeToPG(backup.FinishedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("databases: finish backup: %w", err)
	}
	return backupFromRow(row), nil
}

// DeleteBackup implements BackupRepository.
func (r *storeBackupRepository) DeleteBackup(ctx context.Context, backupID uuid.UUID) (Backup, error) {
	row, err := r.store.DeleteBackup(ctx, pgUUID(backupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, ErrNotFound
		}
		return Backup{}, fmt.Errorf("databases: delete backup: %w", err)
	}
	return backupFromRow(row), nil
}

// SetBackupWasRunning implements BackupRepository.
func (r *storeBackupRepository) SetBackupWasRunning(ctx context.Context, backupID uuid.UUID, wasRunning bool) error {
	if err := r.store.SetBackupWasRunning(ctx, sqlc.SetBackupWasRunningParams{
		ID:         pgUUID(backupID),
		WasRunning: wasRunning,
	}); err != nil {
		return fmt.Errorf("databases: record backup was_running: %w", err)
	}
	return nil
}

// CreateRestore implements BackupRepository.
func (r *storeBackupRepository) CreateRestore(ctx context.Context, restore Restore) (Restore, error) {
	row, err := r.store.CreateRestore(ctx, sqlc.CreateRestoreParams{
		ID:         pgUUID(restore.ID),
		DatabaseID: pgUUID(restore.DatabaseID),
		BackupID:   pgUUID(restore.BackupID),
		Status:     string(restore.Status),
	})
	if err != nil {
		return Restore{}, fmt.Errorf("databases: create restore: %w", err)
	}
	return restoreFromRow(row), nil
}

// FinishRestore implements BackupRepository.
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
		return Restore{}, fmt.Errorf("databases: finish restore: %w", err)
	}
	return restoreFromRow(row), nil
}

// ListRunningRestores implements BackupRepository.
func (r *storeBackupRepository) ListRunningRestores(ctx context.Context) ([]Restore, error) {
	rows, err := r.store.ListRunningRestores(ctx)
	if err != nil {
		return nil, fmt.Errorf("databases: list running restores: %w", err)
	}
	restores := make([]Restore, 0, len(rows))
	for _, row := range rows {
		restores = append(restores, restoreFromRow(row))
	}
	return restores, nil
}

// CreateBackupSchedule implements BackupRepository.
func (r *storeBackupRepository) CreateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error) {
	row, err := r.store.CreateBackupSchedule(ctx, sqlc.CreateBackupScheduleParams{
		ID:         pgUUID(schedule.ID),
		DatabaseID: pgUUID(schedule.DatabaseID),
		Cron:       schedule.Cron,
		TargetID:   pgUUID(schedule.TargetID),
		Enabled:    schedule.Enabled,
		NextRunAt:  timeToPG(schedule.NextRunAt),
	})
	if err != nil {
		return BackupSchedule{}, fmt.Errorf("databases: create backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

// GetBackupSchedule implements BackupRepository.
func (r *storeBackupRepository) GetBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	row, err := r.store.GetBackupSchedule(ctx, pgUUID(scheduleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("databases: get backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

// ListBackupSchedulesByDatabase implements BackupRepository.
func (r *storeBackupRepository) ListBackupSchedulesByDatabase(ctx context.Context, databaseID uuid.UUID) ([]BackupSchedule, error) {
	rows, err := r.store.ListBackupSchedulesByDatabase(ctx, pgUUID(databaseID))
	if err != nil {
		return nil, fmt.Errorf("databases: list backup schedules: %w", err)
	}
	schedules := make([]BackupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row))
	}
	return schedules, nil
}

// UpdateBackupSchedule implements BackupRepository.
func (r *storeBackupRepository) UpdateBackupSchedule(ctx context.Context, schedule BackupSchedule) (BackupSchedule, error) {
	row, err := r.store.UpdateBackupSchedule(ctx, sqlc.UpdateBackupScheduleParams{
		ID:        pgUUID(schedule.ID),
		Cron:      schedule.Cron,
		TargetID:  pgUUID(schedule.TargetID),
		Enabled:   schedule.Enabled,
		NextRunAt: timeToPG(schedule.NextRunAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("databases: update backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

// DeleteBackupSchedule implements BackupRepository.
func (r *storeBackupRepository) DeleteBackupSchedule(ctx context.Context, scheduleID uuid.UUID) (BackupSchedule, error) {
	row, err := r.store.DeleteBackupSchedule(ctx, pgUUID(scheduleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupSchedule{}, ErrNotFound
		}
		return BackupSchedule{}, fmt.Errorf("databases: delete backup schedule: %w", err)
	}
	return scheduleFromRow(row), nil
}

// ListDueBackupSchedules implements BackupRepository.
func (r *storeBackupRepository) ListDueBackupSchedules(ctx context.Context, now time.Time) ([]BackupSchedule, error) {
	rows, err := r.store.ListDueBackupSchedules(ctx, timeToPG(now.UTC()))
	if err != nil {
		return nil, fmt.Errorf("databases: list due backup schedules: %w", err)
	}
	schedules := make([]BackupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row))
	}
	return schedules, nil
}

// MarkBackupScheduleRun implements BackupRepository.
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
		return BackupSchedule{}, fmt.Errorf("databases: mark backup schedule run: %w", err)
	}
	return scheduleFromRow(row), nil
}

// CreateBackupTarget implements BackupRepository.
func (r *storeBackupRepository) CreateBackupTarget(ctx context.Context, target BackupTarget) (BackupTarget, error) {
	row, err := r.store.CreateBackupTarget(ctx, sqlc.CreateBackupTargetParams{
		ID:       pgUUID(target.ID),
		UserID:   pgUUID(target.UserID),
		Name:     target.Name,
		Kind:     string(target.Kind),
		Endpoint: target.Endpoint,
		Region:   target.Region,
		Bucket:   target.Bucket,
		Prefix:   target.Prefix,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return BackupTarget{}, ErrConflict
		}
		return BackupTarget{}, fmt.Errorf("databases: create backup target: %w", err)
	}
	return targetFromRow(row), nil
}

// GetBackupTarget implements BackupRepository.
func (r *storeBackupRepository) GetBackupTarget(ctx context.Context, targetID uuid.UUID) (BackupTarget, error) {
	row, err := r.store.GetBackupTarget(ctx, pgUUID(targetID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupTarget{}, ErrNotFound
		}
		return BackupTarget{}, fmt.Errorf("databases: get backup target: %w", err)
	}
	return targetFromRow(row), nil
}

// ListBackupTargetsByUser implements BackupRepository.
func (r *storeBackupRepository) ListBackupTargetsByUser(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error) {
	rows, err := r.store.ListBackupTargetsByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("databases: list backup targets: %w", err)
	}
	targets := make([]BackupTarget, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, targetFromRow(row))
	}
	return targets, nil
}

// UpdateBackupTarget implements BackupRepository.
func (r *storeBackupRepository) UpdateBackupTarget(ctx context.Context, target BackupTarget) (BackupTarget, error) {
	row, err := r.store.UpdateBackupTarget(ctx, sqlc.UpdateBackupTargetParams{
		ID:       pgUUID(target.ID),
		Name:     target.Name,
		Kind:     string(target.Kind),
		Endpoint: target.Endpoint,
		Region:   target.Region,
		Bucket:   target.Bucket,
		Prefix:   target.Prefix,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return BackupTarget{}, ErrConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupTarget{}, ErrNotFound
		}
		return BackupTarget{}, fmt.Errorf("databases: update backup target: %w", err)
	}
	return targetFromRow(row), nil
}

// DeleteBackupTarget implements BackupRepository. The update query filters by
// owner, so another user's target is simply not there.
func (r *storeBackupRepository) DeleteBackupTarget(ctx context.Context, targetID, userID uuid.UUID) (BackupTarget, error) {
	row, err := r.store.DeleteBackupTarget(ctx, sqlc.DeleteBackupTargetParams{
		ID:     pgUUID(targetID),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackupTarget{}, ErrNotFound
		}
		return BackupTarget{}, fmt.Errorf("databases: delete backup target: %w", err)
	}
	return targetFromRow(row), nil
}

// CreateTargetSecret implements BackupRepository.
func (r *storeBackupRepository) CreateTargetSecret(ctx context.Context, secret TargetSecret) (TargetSecret, error) {
	row, err := r.store.CreateBackupTargetSecret(ctx, sqlc.CreateBackupTargetSecretParams{
		TargetID:   pgUUID(secret.TargetID),
		Key:        secret.Key,
		Ciphertext: secret.Ciphertext,
	})
	if err != nil {
		return TargetSecret{}, fmt.Errorf("databases: create backup target secret: %w", err)
	}
	return targetSecretFromRow(row), nil
}

// UpsertTargetSecret implements BackupRepository.
func (r *storeBackupRepository) UpsertTargetSecret(ctx context.Context, secret TargetSecret) (TargetSecret, error) {
	row, err := r.store.UpsertBackupTargetSecret(ctx, sqlc.UpsertBackupTargetSecretParams{
		TargetID:   pgUUID(secret.TargetID),
		Key:        secret.Key,
		Ciphertext: secret.Ciphertext,
	})
	if err != nil {
		return TargetSecret{}, fmt.Errorf("databases: upsert backup target secret: %w", err)
	}
	return targetSecretFromRow(row), nil
}

// ListTargetSecrets implements BackupRepository.
func (r *storeBackupRepository) ListTargetSecrets(ctx context.Context, targetID uuid.UUID) ([]TargetSecret, error) {
	rows, err := r.store.ListBackupTargetSecrets(ctx, pgUUID(targetID))
	if err != nil {
		return nil, fmt.Errorf("databases: list backup target secrets: %w", err)
	}
	secrets := make([]TargetSecret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, targetSecretFromRow(row))
	}
	return secrets, nil
}

// backupFromRow maps one sqlc backups row onto the domain type.
func backupFromRow(row sqlc.Backup) Backup {
	return Backup{
		ID:          uuidFromPG(row.ID),
		DatabaseID:  uuidFromPG(row.DatabaseID),
		ScheduleID:  uuidFromPG(row.ScheduleID),
		Type:        BackupType(row.Type),
		Status:      BackupStatus(row.Status),
		Size:        row.Size,
		Location:    row.Location,
		TargetID:    uuidFromPG(row.TargetID),
		ContainerID: row.ContainerID,
		WasRunning:  row.WasRunning,
		Error:       row.Error,
		CreatedAt:   timeFromPG(row.CreatedAt),
		FinishedAt:  timeFromPG(row.FinishedAt),
	}
}

// restoreFromRow maps one sqlc restores row onto the domain type.
func restoreFromRow(row sqlc.Restore) Restore {
	return Restore{
		ID:         uuidFromPG(row.ID),
		DatabaseID: uuidFromPG(row.DatabaseID),
		BackupID:   uuidFromPG(row.BackupID),
		Status:     RestoreStatus(row.Status),
		Error:      row.Error,
		CreatedAt:  timeFromPG(row.CreatedAt),
		FinishedAt: timeFromPG(row.FinishedAt),
	}
}

// scheduleFromRow maps one sqlc backup_schedules row onto the domain type.
func scheduleFromRow(row sqlc.BackupSchedule) BackupSchedule {
	return BackupSchedule{
		ID:         uuidFromPG(row.ID),
		DatabaseID: uuidFromPG(row.DatabaseID),
		Cron:       row.Cron,
		TargetID:   uuidFromPG(row.TargetID),
		Enabled:    row.Enabled,
		LastRunAt:  timeFromPG(row.LastRunAt),
		NextRunAt:  timeFromPG(row.NextRunAt),
		CreatedAt:  timeFromPG(row.CreatedAt),
		UpdatedAt:  timeFromPG(row.UpdatedAt),
	}
}

// targetFromRow maps one sqlc backup_targets row onto the domain type.
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
		CreatedAt: timeFromPG(row.CreatedAt),
		UpdatedAt: timeFromPG(row.UpdatedAt),
	}
}

// targetSecretFromRow maps one sqlc backup_target_secrets row.
func targetSecretFromRow(row sqlc.BackupTargetSecret) TargetSecret {
	return TargetSecret{
		ID:         uuidFromPG(row.ID),
		TargetID:   uuidFromPG(row.TargetID),
		Key:        row.Key,
		Ciphertext: row.Ciphertext,
		CreatedAt:  timeFromPG(row.CreatedAt),
	}
}

// timeToPG converts a domain timestamp for sqlc. The zero value becomes an
// invalid pgtype value, which serialises as NULL for nullable columns.
func timeToPG(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
