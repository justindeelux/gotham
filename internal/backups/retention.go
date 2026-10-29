package backups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Retention limits live here so every threshold is documented in one place.
const (
	// DefaultKeepLastScheduledBackups is what a newly created schedule keeps.
	DefaultKeepLastScheduledBackups = 7
	MinKeepLastScheduledBackups     = 1
	MaxKeepLastScheduledBackups     = 365

	// FailedBackupRetention is how long a failed run (no useful artifact) is
	// kept before the cleanup sweeper prunes it.
	FailedBackupRetention = 30 * 24 * time.Hour

	// CleanupSweepInterval is how often pending artifact cleanups are retried.
	CleanupSweepInterval = time.Hour

	cleanupRetryBase = time.Minute
	cleanupRetryMax  = time.Hour

	// localDiskMinHeadroom is the smallest space a backup may claim on the
	// Local disk provider when the resource has no previous completed run.
	localDiskMinHeadroom = 64 << 20
	localDiskHeadroomPct = 20
)

func normalizeKeepLast(value *int, fallback int) (*int, error) {
	if value == nil {
		if fallback == 0 {
			return nil, nil
		}
		kept := fallback
		return &kept, nil
	}
	if *value < MinKeepLastScheduledBackups || *value > MaxKeepLastScheduledBackups {
		return nil, fmt.Errorf("%w: keep_last must be between %d and %d",
			ErrValidation, MinKeepLastScheduledBackups, MaxKeepLastScheduledBackups)
	}
	kept := *value
	return &kept, nil
}

// requestCleanup removes a run through the pending-delete path: the row is
// marked first so a provider outage leaves a retryable record instead of an
// untracked artifact.
func (m *BackupManager) requestCleanup(ctx context.Context, backup Backup) error {
	if backup.Status == BackupRunning {
		return fmt.Errorf("%w: backup is still running", ErrBackupInFlight)
	}
	pending, err := m.backups.SetBackupCleanupPending(ctx, backup.ID, m.now())
	if err != nil {
		return err
	}
	return m.deleteArtifactThenRow(ctx, pending)
}

func (m *BackupManager) deleteArtifactThenRow(ctx context.Context, backup Backup) error {
	if backup.Location != "" {
		store, err := m.storeForLocation(ctx, backup, uuid.Nil)
		if err != nil {
			return m.recordCleanupError(ctx, backup, err)
		}
		if err := store.Delete(ctx, backup.Location); err != nil {
			return m.recordCleanupError(ctx, backup, err)
		}
	}
	if _, err := m.backups.DeleteBackup(ctx, backup.ID); err != nil {
		return err
	}
	return nil
}

// recordCleanupError keeps the row pending with a backoff. It never marks the
// run failed: the backup itself succeeded, only its removal is outstanding.
func (m *BackupManager) recordCleanupError(ctx context.Context, backup Backup, cause error) error {
	attempts := backup.CleanupAttempts + 1
	next := m.now().Add(cleanupBackoff(attempts))
	if _, err := m.backups.SetBackupCleanupError(ctx, backup.ID,
		boundedDiag(cause.Error()), attempts, next); err != nil {
		m.logger.Error("backups: could not record a cleanup failure",
			"backup_id", backup.ID.String(), "error", err)
	}
	return fmt.Errorf("%w: %v", ErrCleanupFailed, cause)
}

func cleanupBackoff(attempts int) time.Duration {
	delay := cleanupRetryBase
	for i := 1; i < attempts && delay < cleanupRetryMax; i++ {
		delay *= 2
	}
	if delay > cleanupRetryMax {
		return cleanupRetryMax
	}
	return delay
}

// pruneSchedule deletes completed scheduled runs beyond the schedule's
// keep_last, oldest first. Manual runs, failed runs, the newest completed run
// and any running run are never touched because the query only returns
// completed scheduled runs and keep_last is at least 1.
func (m *BackupManager) pruneSchedule(ctx context.Context, scheduleID uuid.UUID) {
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return
	}
	if schedule.KeepLast == nil {
		return
	}
	keep := *schedule.KeepLast
	if keep < MinKeepLastScheduledBackups {
		keep = MinKeepLastScheduledBackups
	}
	completed, err := m.backups.ListCompletedScheduledBackups(ctx, scheduleID)
	if err != nil {
		m.logger.Warn("backups: could not list runs for retention",
			"schedule_id", scheduleID.String(), "error", err)
		return
	}
	for index := keep; index < len(completed); index++ {
		if err := m.requestCleanup(ctx, completed[index]); err != nil {
			m.logger.Warn("backups: could not prune an old scheduled backup",
				"backup_id", completed[index].ID.String(), "schedule_id", scheduleID.String(),
				"error", err)
		}
	}
}

// SweepCleanup drains pending artifact cleanups and prunes old failed runs. It
// is safe to call directly from tests and the hourly sweeper.
func (m *BackupManager) SweepCleanup(ctx context.Context) (int, error) {
	if m == nil || m.backups == nil {
		return 0, fmt.Errorf("backups: backup repository is not configured")
	}
	removed := 0
	now := m.now()

	pending, err := m.backups.ListDuePendingCleanups(ctx, now)
	if err != nil {
		return removed, err
	}
	for _, backup := range pending {
		if err := m.deleteArtifactThenRow(ctx, backup); err != nil {
			m.logger.Warn("backups: pending cleanup still failing",
				"backup_id", backup.ID.String(), "error", err)
			continue
		}
		removed++
	}

	failed, err := m.backups.ListFailedBackupsOlderThan(ctx, now.Add(-FailedBackupRetention))
	if err != nil {
		return removed, err
	}
	for _, backup := range failed {
		if err := m.requestCleanup(ctx, backup); err != nil {
			m.logger.Warn("backups: could not prune a failed run",
				"backup_id", backup.ID.String(), "error", err)
			continue
		}
		removed++
	}

	orphans, err := m.backups.ListDueArtifactCleanups(ctx, now)
	if err != nil {
		return removed, err
	}
	for _, item := range orphans {
		if err := m.deleteQueuedArtifact(ctx, item); err != nil {
			m.logger.Warn("backups: orphaned artifact cleanup still failing",
				"artifact", item.Location, "error", err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (m *BackupManager) deleteQueuedArtifact(ctx context.Context, item ArtifactCleanup) error {
	store, err := m.storeForCleanupLocation(ctx, item)
	if err != nil {
		// The provider row is gone: the artifact is unreachable by design and
		// deleting the queue entry is the only honest outcome.
		if isMissingTarget(err) {
			m.logger.Warn("backups: dropping an artifact whose provider was removed",
				"artifact", item.Location)
			return m.backups.DeleteArtifactCleanup(ctx, item.ID)
		}
		return m.queueArtifactError(ctx, item, err)
	}
	if strings.TrimSpace(item.Location) != "" {
		if err := store.Delete(ctx, item.Location); err != nil {
			return m.queueArtifactError(ctx, item, err)
		}
	}
	return m.backups.DeleteArtifactCleanup(ctx, item.ID)
}

func (m *BackupManager) queueArtifactError(ctx context.Context, item ArtifactCleanup, cause error) error {
	attempts := item.Attempts + 1
	if err := m.backups.SetArtifactCleanupError(ctx, item.ID,
		boundedDiag(cause.Error()), attempts, m.now().Add(cleanupBackoff(attempts))); err != nil {
		return err
	}
	// The artifact is still stored, so callers must not count it as removed.
	return fmt.Errorf("%w: %v", ErrCleanupFailed, cause)
}

func (m *BackupManager) storeForCleanupLocation(ctx context.Context, item ArtifactCleanup) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	switch {
	case strings.HasPrefix(item.Location, locationFilePrefix):
		return newLocalStore(m.localDir)
	case strings.HasPrefix(item.Location, locationS3Prefix):
		if item.TargetID == uuid.Nil {
			return nil, fmt.Errorf("%w: the storage provider of this artifact no longer exists", ErrValidation)
		}
		target, err := m.backups.GetBackupTarget(ctx, item.TargetID)
		if err != nil {
			return nil, err
		}
		return m.storeForTarget(ctx, &target)
	default:
		return nil, fmt.Errorf("%w: unknown artifact location %q", ErrValidation, item.Location)
	}
}

func isMissingTarget(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrValidation)
}

// checkLocalSpace fails a run before it starts dumping when the Local disk
// provider cannot hold roughly what the previous run of this resource used.
func (m *BackupManager) checkLocalSpace(ctx context.Context, resource Resource) error {
	if m.objects != nil {
		return nil
	}
	free, err := diskFreeBytes(m.localDir)
	if err != nil {
		// Unknown free space must not block an otherwise valid backup.
		m.logger.Warn("backups: could not read free space", "dir", m.localDir, "error", err)
		return nil
	}
	required := int64(localDiskMinHeadroom)
	previous, err := m.backups.ListBackupsByResource(ctx, resource.Ref(), BackupCompleted, 1, 0)
	if err == nil && len(previous) > 0 && previous[0].Size > 0 {
		required = previous[0].Size + previous[0].Size*localDiskHeadroomPct/100
	}
	if free < required {
		return fmt.Errorf("%w: %s free, about %s needed", ErrInsufficientSpace,
			humanBytes(free), humanBytes(required))
	}
	return nil
}

func diskFreeBytes(dir string) (int64, error) {
	free, _, err := diskStats(dir)
	return free, err
}

func humanBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	labels := []string{"KiB", "MiB", "GiB", "TiB"}
	label := labels[0]
	for _, next := range labels {
		label = next
		value /= unit
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, label)
}

type cleanupSweeper struct {
	manager  *BackupManager
	interval time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	closed  bool
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

func newCleanupSweeper(manager *BackupManager) *cleanupSweeper {
	logger := manager.logger
	if logger == nil {
		logger = slog.Default()
	}
	return &cleanupSweeper{manager: manager, interval: CleanupSweepInterval, logger: logger}
}

func (s *cleanupSweeper) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running || s.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.wait.Add(1)
	go s.loop(ctx)
}

func (s *cleanupSweeper) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.closed = true
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wait.Wait()
}

func (s *cleanupSweeper) loop(ctx context.Context) {
	defer s.wait.Done()
	s.sweepOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *cleanupSweeper) sweepOnce(ctx context.Context) {
	removed, err := s.manager.SweepCleanup(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("backups: cleanup sweep failed", "error", err)
		}
		return
	}
	if removed > 0 {
		s.logger.Info("backups: cleanup sweep removed old artifacts", "removed", removed)
	}
}
