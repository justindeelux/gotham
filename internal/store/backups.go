package store

import (
	"context"

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

func (s *Store) ListBackupsByDatabase(ctx context.Context, databaseID pgtype.UUID, limit int32) ([]sqlc.Backup, error) {
	return s.queries.ListBackupsByDatabase(ctx, sqlc.ListBackupsByDatabaseParams{
		DatabaseID: databaseID,
		Limit:      limit,
	})
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

func (s *Store) CreateRestore(ctx context.Context, params sqlc.CreateRestoreParams) (sqlc.Restore, error) {
	return s.queries.CreateRestore(ctx, params)
}

func (s *Store) FinishRestore(ctx context.Context, params sqlc.FinishRestoreParams) (sqlc.Restore, error) {
	return s.queries.FinishRestore(ctx, params)
}

func (s *Store) ListRunningRestores(ctx context.Context) ([]sqlc.Restore, error) {
	return s.queries.ListRunningRestores(ctx)
}

func (s *Store) ListRestoresByDatabase(ctx context.Context, params sqlc.ListRestoresByDatabaseParams) ([]sqlc.Restore, error) {
	return s.queries.ListRestoresByDatabase(ctx, params)
}

func (s *Store) CreateBackupSchedule(ctx context.Context, params sqlc.CreateBackupScheduleParams) (sqlc.BackupSchedule, error) {
	return s.queries.CreateBackupSchedule(ctx, params)
}

func (s *Store) GetBackupSchedule(ctx context.Context, id pgtype.UUID) (sqlc.BackupSchedule, error) {
	return s.queries.GetBackupSchedule(ctx, id)
}

func (s *Store) ListBackupSchedulesByDatabase(ctx context.Context, databaseID pgtype.UUID) ([]sqlc.BackupSchedule, error) {
	return s.queries.ListBackupSchedulesByDatabase(ctx, databaseID)
}

func (s *Store) UpdateBackupSchedule(ctx context.Context, params sqlc.UpdateBackupScheduleParams) (sqlc.BackupSchedule, error) {
	return s.queries.UpdateBackupSchedule(ctx, params)
}

func (s *Store) DeleteBackupSchedule(ctx context.Context, id pgtype.UUID) (sqlc.BackupSchedule, error) {
	return s.queries.DeleteBackupSchedule(ctx, id)
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

func (s *Store) CreateBackupTargetSecret(ctx context.Context, params sqlc.CreateBackupTargetSecretParams) (sqlc.BackupTargetSecret, error) {
	return s.queries.CreateBackupTargetSecret(ctx, params)
}

func (s *Store) UpsertBackupTargetSecret(ctx context.Context, params sqlc.UpsertBackupTargetSecretParams) (sqlc.BackupTargetSecret, error) {
	return s.queries.UpsertBackupTargetSecret(ctx, params)
}

func (s *Store) ListBackupTargetSecrets(ctx context.Context, targetID pgtype.UUID) ([]sqlc.BackupTargetSecret, error) {
	return s.queries.ListBackupTargetSecrets(ctx, targetID)
}
