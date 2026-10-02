package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Thin wrappers over the generated backup queries. Store keeps its sqlc
// handle private, so every package that persists reads through methods like
// these; the database-side equivalent is databases.go.

// CreateBackup stores one backup run with its caller-generated ID.
func (s *Store) CreateBackup(ctx context.Context, params sqlc.CreateBackupParams) (sqlc.Backup, error) {
	return s.queries.CreateBackup(ctx, params)
}

// GetBackup returns a backup run whose database is still live; ownership is
// checked by the caller through the database row.
func (s *Store) GetBackup(ctx context.Context, id pgtype.UUID) (sqlc.Backup, error) {
	return s.queries.GetBackup(ctx, id)
}

// ListBackupsByDatabase returns a database's runs, newest first.
func (s *Store) ListBackupsByDatabase(ctx context.Context, databaseID pgtype.UUID) ([]sqlc.Backup, error) {
	return s.queries.ListBackupsByDatabase(ctx, databaseID)
}

// ListRunningBackups returns every run the control plane left running.
func (s *Store) ListRunningBackups(ctx context.Context) ([]sqlc.Backup, error) {
	return s.queries.ListRunningBackups(ctx)
}

// FinishBackup persists the terminal state of a run (status, size, location,
// error, container) and returns the row.
func (s *Store) FinishBackup(ctx context.Context, params sqlc.FinishBackupParams) (sqlc.Backup, error) {
	return s.queries.FinishBackup(ctx, params)
}

// DeleteBackup removes a run and returns the deleted row.
func (s *Store) DeleteBackup(ctx context.Context, id pgtype.UUID) (sqlc.Backup, error) {
	return s.queries.DeleteBackup(ctx, id)
}

// CreateRestore stores one restore run before its job starts.
func (s *Store) CreateRestore(ctx context.Context, params sqlc.CreateRestoreParams) (sqlc.Restore, error) {
	return s.queries.CreateRestore(ctx, params)
}

// FinishRestore persists the terminal state of a restore run and returns it.
func (s *Store) FinishRestore(ctx context.Context, params sqlc.FinishRestoreParams) (sqlc.Restore, error) {
	return s.queries.FinishRestore(ctx, params)
}

// ListRunningRestores returns every restore the control plane left running.
func (s *Store) ListRunningRestores(ctx context.Context) ([]sqlc.Restore, error) {
	return s.queries.ListRunningRestores(ctx)
}

// CreateBackupSchedule stores a schedule with its computed next run.
func (s *Store) CreateBackupSchedule(ctx context.Context, params sqlc.CreateBackupScheduleParams) (sqlc.BackupSchedule, error) {
	return s.queries.CreateBackupSchedule(ctx, params)
}

// GetBackupSchedule returns a schedule whose database is still live.
func (s *Store) GetBackupSchedule(ctx context.Context, id pgtype.UUID) (sqlc.BackupSchedule, error) {
	return s.queries.GetBackupSchedule(ctx, id)
}

// ListBackupSchedulesByDatabase returns a database's schedules, newest first.
func (s *Store) ListBackupSchedulesByDatabase(ctx context.Context, databaseID pgtype.UUID) ([]sqlc.BackupSchedule, error) {
	return s.queries.ListBackupSchedulesByDatabase(ctx, databaseID)
}

// UpdateBackupSchedule persists the mutable schedule fields and returns the
// row; a missing row reports pgx.ErrNoRows.
func (s *Store) UpdateBackupSchedule(ctx context.Context, params sqlc.UpdateBackupScheduleParams) (sqlc.BackupSchedule, error) {
	return s.queries.UpdateBackupSchedule(ctx, params)
}

// DeleteBackupSchedule removes a schedule and returns the deleted row.
func (s *Store) DeleteBackupSchedule(ctx context.Context, id pgtype.UUID) (sqlc.BackupSchedule, error) {
	return s.queries.DeleteBackupSchedule(ctx, id)
}

// ListDueBackupSchedules returns enabled schedules whose next run has reached
// the given instant, oldest first — the scheduler's single query.
func (s *Store) ListDueBackupSchedules(ctx context.Context, nextRunAt pgtype.Timestamptz) ([]sqlc.BackupSchedule, error) {
	return s.queries.ListDueBackupSchedules(ctx, nextRunAt)
}

// MarkBackupScheduleRun records a completed tick and the next due time.
func (s *Store) MarkBackupScheduleRun(ctx context.Context, params sqlc.MarkBackupScheduleRunParams) (sqlc.BackupSchedule, error) {
	return s.queries.MarkBackupScheduleRun(ctx, params)
}

// CreateBackupTarget stores a storage target without its credentials (those
// are sealed rows in backup_target_secrets).
func (s *Store) CreateBackupTarget(ctx context.Context, params sqlc.CreateBackupTargetParams) (sqlc.BackupTarget, error) {
	return s.queries.CreateBackupTarget(ctx, params)
}

// GetBackupTarget returns one storage target.
func (s *Store) GetBackupTarget(ctx context.Context, id pgtype.UUID) (sqlc.BackupTarget, error) {
	return s.queries.GetBackupTarget(ctx, id)
}

// ListBackupTargetsByUser returns a user's targets, newest first.
func (s *Store) ListBackupTargetsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.BackupTarget, error) {
	return s.queries.ListBackupTargetsByUser(ctx, userID)
}

// UpdateBackupTarget persists a target's configuration and returns the row.
func (s *Store) UpdateBackupTarget(ctx context.Context, params sqlc.UpdateBackupTargetParams) (sqlc.BackupTarget, error) {
	return s.queries.UpdateBackupTarget(ctx, params)
}

// DeleteBackupTarget removes a target owned by userID, returning pgx.ErrNoRows
// when the row is missing or belongs to someone else.
func (s *Store) DeleteBackupTarget(ctx context.Context, params sqlc.DeleteBackupTargetParams) (sqlc.BackupTarget, error) {
	return s.queries.DeleteBackupTarget(ctx, params)
}

// CreateBackupTargetSecret stores one sealed credential of a target.
func (s *Store) CreateBackupTargetSecret(ctx context.Context, params sqlc.CreateBackupTargetSecretParams) (sqlc.BackupTargetSecret, error) {
	return s.queries.CreateBackupTargetSecret(ctx, params)
}

// UpsertBackupTargetSecret stores or replaces one sealed credential, which is
// how a rotated access key is written.
func (s *Store) UpsertBackupTargetSecret(ctx context.Context, params sqlc.UpsertBackupTargetSecretParams) (sqlc.BackupTargetSecret, error) {
	return s.queries.UpsertBackupTargetSecret(ctx, params)
}

// ListBackupTargetSecrets returns a target's sealed credentials, sorted by
// key. The ciphertext is opened by the databases package, never here.
func (s *Store) ListBackupTargetSecrets(ctx context.Context, targetID pgtype.UUID) ([]sqlc.BackupTargetSecret, error) {
	return s.queries.ListBackupTargetSecrets(ctx, targetID)
}
