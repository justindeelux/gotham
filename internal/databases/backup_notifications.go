package databases

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const backupNotifyTimeout = 5 * time.Second

type BackupResult struct {
	BackupID   uuid.UUID
	DatabaseID uuid.UUID
	Database   string
	TeamID     uuid.UUID
	Status     BackupStatus
	Error      string
	FinishedAt time.Time
}

type BackupNotifier interface {
	BackupFinished(ctx context.Context, result BackupResult)
}

func (m *BackupManager) notifyBackup(ctx context.Context, finished Backup, database Database) {
	if m == nil || m.notifier == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backupNotifyTimeout)
	defer cancel()
	m.notifier.BackupFinished(notifyCtx, BackupResult{
		BackupID:   finished.ID,
		DatabaseID: database.ID,
		Database:   database.Name,
		TeamID:     database.TeamID,
		Status:     finished.Status,
		Error:      finished.Error,
		FinishedAt: finished.FinishedAt,
	})
}
