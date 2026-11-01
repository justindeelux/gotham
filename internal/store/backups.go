package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateBackup(ctx context.Context, params sqlc.CreateBackupParams) (sqlc.Backup, error) {
	return s.queries.CreateBackup(ctx, params)
}

func (s *Store) CreateBackupWithTarget(ctx context.Context, params sqlc.CreateBackupParams, validate func(sqlc.BackupTarget) error) (sqlc.Backup, *sqlc.BackupTarget, error) {
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
		if validate != nil {
			if err := validate(row); err != nil {
				return sqlc.Backup{}, nil, err
			}
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

func (s *Store) GetBackup(ctx context.Context, id pgtype.UUID) (sqlc.Backup, error) {
	return s.queries.GetBackup(ctx, id)
}

func (s *Store) ListBackupsByResource(ctx context.Context, params sqlc.ListBackupsByResourceParams) ([]sqlc.Backup, error) {
	return s.queries.ListBackupsByResource(ctx, params)
}

func (s *Store) ListBackups(ctx context.Context, params sqlc.ListBackupsParams) ([]sqlc.Backup, error) {
	return s.queries.ListBackups(ctx, params)
}

func (s *Store) HasBackupsForTarget(ctx context.Context, targetID pgtype.UUID) (bool, error) {
	return s.queries.HasBackupsForTarget(ctx, targetID)
}

func (s *Store) ListRunningBackups(ctx context.Context) ([]sqlc.Backup, error) {
	return s.queries.ListRunningBackups(ctx)
}

func (s *Store) FinishBackup(ctx context.Context, params sqlc.FinishBackupParams) (sqlc.Backup, error) {
	return s.queries.FinishBackup(ctx, params)
}

func (s *Store) DeleteBackup(ctx context.Context, id pgtype.UUID) (sqlc.Backup, error) {
	return s.queries.DeleteBackup(ctx, id)
}

func (s *Store) SetBackupWasRunning(ctx context.Context, params sqlc.SetBackupWasRunningParams) error {
	return s.queries.SetBackupWasRunning(ctx, params)
}

func (s *Store) ListCompletedScheduledBackups(ctx context.Context, scheduleID pgtype.UUID) ([]sqlc.Backup, error) {
	return s.queries.ListCompletedScheduledBackups(ctx, scheduleID)
}

func (s *Store) ListFailedBackupsOlderThan(ctx context.Context, before pgtype.Timestamptz) ([]sqlc.Backup, error) {
	return s.queries.ListFailedBackupsOlderThan(ctx, before)
}

func (s *Store) SetBackupCleanupPending(ctx context.Context, params sqlc.SetBackupCleanupPendingParams) (sqlc.Backup, error) {
	return s.queries.SetBackupCleanupPending(ctx, params)
}

func (s *Store) SetBackupCleanupError(ctx context.Context, params sqlc.SetBackupCleanupErrorParams) (sqlc.Backup, error) {
	return s.queries.SetBackupCleanupError(ctx, params)
}

func (s *Store) ListDuePendingCleanups(ctx context.Context, now pgtype.Timestamptz) ([]sqlc.Backup, error) {
	return s.queries.ListDuePendingCleanups(ctx, now)
}

func (s *Store) MarkBackupsProviderRemoved(ctx context.Context, targetID pgtype.UUID) error {
	return s.queries.MarkBackupsProviderRemoved(ctx, targetID)
}

func (s *Store) CountBackupsForTarget(ctx context.Context, targetID pgtype.UUID) (int64, error) {
	return s.queries.CountBackupsForTarget(ctx, targetID)
}

func (s *Store) ListBackupScheduleIDsForTarget(ctx context.Context, targetID pgtype.UUID) ([]pgtype.UUID, error) {
	return s.queries.ListBackupScheduleIDsForTarget(ctx, targetID)
}

func (s *Store) BackupUsageByTarget(ctx context.Context, userID pgtype.UUID) ([]sqlc.BackupUsageByTargetRow, error) {
	return s.queries.BackupUsageByTarget(ctx, userID)
}

func (s *Store) ListDueArtifactCleanups(ctx context.Context, now pgtype.Timestamptz) ([]sqlc.BackupArtifactCleanup, error) {
	return s.queries.ListDueArtifactCleanups(ctx, now)
}

func (s *Store) SetArtifactCleanupError(ctx context.Context, params sqlc.SetArtifactCleanupErrorParams) error {
	return s.queries.SetArtifactCleanupError(ctx, params)
}

func (s *Store) DeleteArtifactCleanup(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteArtifactCleanup(ctx, id)
}

func (s *Store) CreateRestore(ctx context.Context, params sqlc.CreateRestoreParams) (sqlc.Restore, error) {
	return s.queries.CreateRestore(ctx, params)
}

func (s *Store) FinishRestore(ctx context.Context, params sqlc.FinishRestoreParams) (sqlc.Restore, error) {
	return s.queries.FinishRestore(ctx, params)
}

func (s *Store) ListRunningRestores(ctx context.Context) ([]sqlc.Restore, error) {
	return s.queries.ListRunningRestores(ctx)
}

func (s *Store) ListRestoresByResource(ctx context.Context, params sqlc.ListRestoresByResourceParams) ([]sqlc.Restore, error) {
	return s.queries.ListRestoresByResource(ctx, params)
}

func (s *Store) ListRestores(ctx context.Context, params sqlc.ListRestoresParams) ([]sqlc.Restore, error) {
	return s.queries.ListRestores(ctx, params)
}

func (s *Store) CreateBackupSchedule(ctx context.Context, params sqlc.CreateBackupScheduleParams) (sqlc.BackupSchedule, error) {
	// A schedule write locks its destination so a concurrent delete cannot
	// pass the in-use guard and NULL the FK underneath the new row.
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.BackupSchedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if params.TargetID.Valid {
		if _, err := queries.GetBackupTargetForShare(ctx, params.TargetID); err != nil {
			return sqlc.BackupSchedule{}, err
		}
	}
	row, err := queries.CreateBackupSchedule(ctx, params)
	if err != nil {
		return sqlc.BackupSchedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.BackupSchedule{}, err
	}
	return row, nil
}

func (s *Store) GetBackupSchedule(ctx context.Context, id pgtype.UUID) (sqlc.BackupSchedule, error) {
	return s.queries.GetBackupSchedule(ctx, id)
}

func (s *Store) ListBackupSchedulesByResource(ctx context.Context, params sqlc.ListBackupSchedulesByResourceParams) ([]sqlc.BackupSchedule, error) {
	return s.queries.ListBackupSchedulesByResource(ctx, params)
}

func (s *Store) ListBackupSchedules(ctx context.Context, viewerID pgtype.UUID) ([]sqlc.BackupSchedule, error) {
	return s.queries.ListBackupSchedules(ctx, viewerID)
}

func (s *Store) UpdateBackupSchedule(ctx context.Context, params sqlc.UpdateBackupScheduleParams) (sqlc.BackupSchedule, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.BackupSchedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if params.TargetID.Valid {
		if _, err := queries.GetBackupTargetForShare(ctx, params.TargetID); err != nil {
			return sqlc.BackupSchedule{}, err
		}
	}
	row, err := queries.UpdateBackupSchedule(ctx, params)
	if err != nil {
		return sqlc.BackupSchedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.BackupSchedule{}, err
	}
	return row, nil
}

func (s *Store) DeleteBackupSchedule(ctx context.Context, id pgtype.UUID) (sqlc.BackupSchedule, error) {
	return s.queries.DeleteBackupSchedule(ctx, id)
}

func (s *Store) MarkBackupSchedulesTargetChanged(ctx context.Context, targetID pgtype.UUID) error {
	return s.queries.MarkBackupSchedulesTargetChanged(ctx, targetID)
}

func (s *Store) ListDueBackupSchedules(ctx context.Context, nextRunAt pgtype.Timestamptz) ([]sqlc.BackupSchedule, error) {
	return s.queries.ListDueBackupSchedules(ctx, nextRunAt)
}

func (s *Store) MarkBackupScheduleRun(ctx context.Context, params sqlc.MarkBackupScheduleRunParams) (sqlc.BackupSchedule, error) {
	return s.queries.MarkBackupScheduleRun(ctx, params)
}

func (s *Store) CreateBackupTarget(ctx context.Context, params sqlc.CreateBackupTargetParams) (sqlc.BackupTarget, error) {
	return s.queries.CreateBackupTarget(ctx, params)
}

func (s *Store) EnsureBuiltinBackupTarget(ctx context.Context, userID pgtype.UUID) (sqlc.BackupTarget, error) {
	return s.queries.EnsureBuiltinBackupTarget(ctx, userID)
}

func (s *Store) GetBuiltinBackupTarget(ctx context.Context, userID pgtype.UUID) (sqlc.BackupTarget, error) {
	return s.queries.GetBuiltinBackupTarget(ctx, userID)
}

func (s *Store) GetBackupTarget(ctx context.Context, id pgtype.UUID) (sqlc.BackupTarget, error) {
	return s.queries.GetBackupTarget(ctx, id)
}

func (s *Store) ListBackupTargetsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.BackupTarget, error) {
	return s.queries.ListBackupTargetsByUser(ctx, userID)
}

func (s *Store) UpdateBackupTarget(ctx context.Context, params sqlc.UpdateBackupTargetParams) (sqlc.BackupTarget, error) {
	return s.queries.UpdateBackupTarget(ctx, params)
}

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

func backupTargetDestinationChanged(current sqlc.BackupTarget, next sqlc.UpdateBackupTargetParams) bool {
	return current.Kind != next.Kind ||
		current.Endpoint != next.Endpoint ||
		current.Region != next.Region ||
		current.Bucket != next.Bucket
}

func (s *Store) DeleteBackupTarget(ctx context.Context, params sqlc.DeleteBackupTargetParams) (sqlc.BackupTarget, error) {
	return s.queries.DeleteBackupTarget(ctx, params)
}

// DeleteBackupTargetGuarded locks the target row, re-checks its schedules and
// runs and then deletes it inside one transaction. Schedule and run inserts
// take FOR SHARE on the same row, so neither can commit between the guard and
// the delete. deleted is false when the row is built-in, still used by a
// schedule, or still holds backups without confirm.
func (s *Store) DeleteBackupTargetGuarded(ctx context.Context, id, userID pgtype.UUID, confirm bool) (sqlc.BackupTarget, []pgtype.UUID, int64, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	current, err := queries.GetBackupTargetForUpdate(ctx, id)
	if err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	if current.UserID != userID {
		return sqlc.BackupTarget{}, nil, 0, false, pgx.ErrNoRows
	}
	if current.Builtin {
		return current, nil, 0, false, nil
	}
	schedules, err := queries.ListBackupScheduleIDsForTarget(ctx, id)
	if err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	if len(schedules) > 0 {
		return current, schedules, 0, false, nil
	}
	backups, err := queries.CountBackupsForTarget(ctx, id)
	if err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	if backups > 0 && !confirm {
		return current, nil, backups, false, nil
	}
	if backups > 0 {
		if err := queries.MarkBackupsProviderRemoved(ctx, id); err != nil {
			return sqlc.BackupTarget{}, nil, 0, false, err
		}
	}
	if err := queries.MarkBackupSchedulesTargetChanged(ctx, id); err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	row, err := queries.DeleteBackupTarget(ctx, sqlc.DeleteBackupTargetParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.BackupTarget{}, nil, 0, false, err
	}
	return row, nil, backups, true, nil
}

func (s *Store) CreateBackupTargetSecret(ctx context.Context, params sqlc.CreateBackupTargetSecretParams) (sqlc.BackupTargetSecret, error) {
	return s.queries.CreateBackupTargetSecret(ctx, params)
}

func (s *Store) UpsertBackupTargetSecret(ctx context.Context, params sqlc.UpsertBackupTargetSecretParams) (sqlc.BackupTargetSecret, error) {
	return s.queries.UpsertBackupTargetSecret(ctx, params)
}

func (s *Store) ListBackupTargetSecrets(ctx context.Context, targetID pgtype.UUID) ([]sqlc.BackupTargetSecret, error) {
	return s.queries.ListBackupTargetSecrets(ctx, targetID)
}

func (s *Store) ListOwnerUserIDs(ctx context.Context) ([]pgtype.UUID, error) {
	return s.queries.ListOwnerUserIDs(ctx)
}
