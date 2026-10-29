package backups

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const backupNotifyTimeout = 5 * time.Second

// BackupResult names the resource a finished run belongs to, so a notifier can
// address it without knowing which driver captured it.
type BackupResult struct {
	BackupID     uuid.UUID
	ResourceType ResourceType
	ResourceID   uuid.UUID
	Name         string
	Status       BackupStatus
	Error        string
	FinishedAt   time.Time
}

type BackupNotifier interface {
	BackupFinished(ctx context.Context, result BackupResult)
}

func (m *BackupManager) notifyBackup(ctx context.Context, finished Backup, resource Resource) {
	if m == nil || m.notifier == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backupNotifyTimeout)
	defer cancel()
	m.notifier.BackupFinished(notifyCtx, BackupResult{
		BackupID:     finished.ID,
		ResourceType: resource.Type,
		ResourceID:   resource.ID,
		Name:         resource.Name,
		Status:       finished.Status,
		Error:        finished.Error,
		FinishedAt:   finished.FinishedAt,
	})
}
