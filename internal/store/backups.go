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

// CreateBackupWithTarget stores a run that references a storage target. When it
// does, the target row is read under a FOR SHARE lock in the same transaction
// and returned, so a concurrent destination edit (which takes FOR UPDATE)
// cannot commit between the target read and the run insert, and the run always
// carries the config the destination lock saw.
func (s *Store) CreateBackupWithTarget(ctx context.Context, params sqlc.CreateBackupParams) (sqlc.Backup, *sqlc.BackupTarget, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Backup{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	var target *sqlc.BackupTarget
	if params.TargetID.Valid {
		row, err := queries.GetBackupTargetForShare(ctx, params.TargetID)
		if err != nil {
			return sqlc.Backup{}, nil, err
		}
		target = &row
		if s.BeforeBackupRunCommit != nil {
			s.BeforeBackupRunCommit()
		}
	}
	backup, err := queries.CreateBackup(ctx, params)
	if err != nil {
		return sqlc.Backup{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Backup{}, nil, err
	}
	return backup, target, nil
}

// GetBackup returns a backup run whose database is still live; ownership is
// checked by the caller through the database row.
func (s *Store) GetBackup(ctx context.Context, id pgtype.UUID) (sqlc.Backup, error) {
	return s.queries.GetBackup(ctx, id)
}

// ListBackupsByDatabase returns up to limit of a database's runs, newest first.
func (s *Store) ListBackupsByDatabase(ctx context.Context, databaseID pgtype.UUID, limit int32) ([]sqlc.Backup, error) {
	return s.queries.ListBackupsByDatabase(ctx, sqlc.ListBackupsByDatabaseParams{
		DatabaseID: databaseID,
		Limit:      limit,
	})
}

// HasBackupsForTarget reports whether any running or completed run references
// the target, which locks its destination against edits.
func (s *Store) HasBackupsForTarget(ctx context.Context, targetID pgtype.UUID) (bool, error) {
	return s.queries.HasBackupsForTarget(ctx, targetID)
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

// CreateBackupTargetWithSecrets stores a target and its sealed credentials in
// one transaction, so a failed credential write can never leave a target
// without the pair its kind requires.
func (s *Store) CreateBackupTargetWithSecrets(ctx context.Context, target sqlc.CreateBackupTargetParams, secrets []sqlc.UpsertBackupTargetSecretParams) (sqlc.BackupTarget, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.BackupTarget{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	row, err := queries.CreateBackupTarget(ctx, target)
	if err != nil {
		return sqlc.BackupTarget{}, err
	}
	for _, secret := range secrets {
		if _, err := queries.UpsertBackupTargetSecret(ctx, secret); err != nil {
			return sqlc.BackupTarget{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.BackupTarget{}, err
	}
	return row, nil
}

// UpdateBackupTargetWithSecrets persists a target's configuration and upserts
// the supplied sealed credentials in one transaction. It locks the target row
// FOR UPDATE, so it serializes with a run start (which holds FOR SHARE on the
// same row): if the destination changed and any running or completed backup
// references the target, stranded is true and nothing is written.
func (s *Store) UpdateBackupTargetWithSecrets(ctx context.Context, target sqlc.UpdateBackupTargetParams, secrets []sqlc.UpsertBackupTargetSecretParams) (sqlc.BackupTarget, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.BackupTarget{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	current, err := queries.GetBackupTargetForUpdate(ctx, target.ID)
	if err != nil {
		return sqlc.BackupTarget{}, false, err
	}
	if backupTargetDestinationChanged(current, target) {
		hasBackups, err := queries.HasBackupsForTarget(ctx, target.ID)
		if err != nil {
			return sqlc.BackupTarget{}, false, err
		}
		if hasBackups {
			return current, true, nil
		}
	}
	row, err := queries.UpdateBackupTarget(ctx, target)
	if err != nil {
		return sqlc.BackupTarget{}, false, err
	}
	for _, secret := range secrets {
		if _, err := queries.UpsertBackupTargetSecret(ctx, secret); err != nil {
			return sqlc.BackupTarget{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.BackupTarget{}, false, err
	}
	return row, false, nil
}

// backupTargetDestinationChanged reports whether an update moves where a
// target's artifacts live (a recorded location names the endpoint and bucket,
// so those must not change once a run references them).
func backupTargetDestinationChanged(current sqlc.BackupTarget, next sqlc.UpdateBackupTargetParams) bool {
	return current.Kind != next.Kind ||
		current.Endpoint != next.Endpoint ||
		current.Region != next.Region ||
		current.Bucket != next.Bucket
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
