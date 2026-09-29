package databases

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// backupNotifyTimeout bounds the terminal hook. The backup row is already
// durable when it runs, so a slow consumer is cut off instead of holding the
// job goroutine.
const backupNotifyTimeout = 5 * time.Second

// BackupResult is the terminal outcome of one backup run, handed to the
// notification hook. It carries no credentials: only names, status and the
// bounded error text.
type BackupResult struct {
	BackupID   uuid.UUID
	DatabaseID uuid.UUID
	Database   string
	TeamID     uuid.UUID
	Status     BackupStatus
	Error      string
	FinishedAt time.Time
}

// BackupNotifier receives one terminal backup result (completed or failed).
// It must not block the backup job; internal/notifications queues the delivery
// on a bounded worker pool. A nil BackupNotifier disables notifications,
// which is also the FEATURE_NOTIFICATIONS=false path.
type BackupNotifier interface {
	BackupFinished(ctx context.Context, result BackupResult)
}

// notifyBackup hands the terminal result to the configured notifier, best
// effort: the run is already recorded and a notification can never fail a
// backup. The job context may already be expired, so the hook runs detached
// but bounded.
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
