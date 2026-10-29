package backups

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func (m *BackupManager) startRun(
	ctx context.Context,
	source Source,
	resource Resource,
	target *BackupTarget,
	typ BackupType,
	scheduleID uuid.UUID,
	pause bool,
) (Backup, error) {
	if err := m.backupsReady(); err != nil {
		return Backup{}, err
	}
	backupID := uuid.New()
	if !m.claimRun(resource.ID, backupID) {
		return Backup{}, ErrBackupInFlight
	}
	backup := Backup{
		ID:          backupID,
		ResourceRef: resource.Ref(),
		ScheduleID:  scheduleID,
		Type:        typ,
		Status:      BackupRunning,
		CreatedAt:   m.now(),
	}
	if target != nil {
		backup.TargetID = target.ID
	}
	stored, liveTarget, err := m.backups.CreateBackupWithTarget(ctx, backup, func(live *BackupTarget) error {
		if live.Kind == TargetS3 && live.UserID != resource.OwnerID {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		m.release(resource.ID)
		return Backup{}, err
	}
	go m.runBackup(stored, source, resource, liveTarget, pause)
	return stored, nil
}

func (m *BackupManager) runBackup(backup Backup, source Source, resource Resource, target *BackupTarget, pause bool) {
	defer m.release(resource.ID)

	ctx, cancel := context.WithTimeout(context.Background(), m.jobTimeout)
	defer cancel()

	location, size, containerID, err := m.capture(ctx, source, resource, target, backup.ID, pause)
	finished := backup
	finished.ContainerID = containerID
	finished.FinishedAt = m.now()
	if err != nil {
		finished.Status = BackupFailed
		finished.Error = boundedDiag(err.Error())
		m.logger.Error("backups: backup failed",
			"backup_id", backup.ID.String(), "resource", resource.Ref().String(),
			"type", string(backup.Type), "error", err)
	} else {
		finished.Status = BackupCompleted
		finished.Location = location
		finished.Size = size
		m.logger.Info("backups: backup completed",
			"backup_id", backup.ID.String(), "resource", resource.Ref().String(),
			"type", string(backup.Type), "size", size, "location", location)
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishCancel()
	if _, ferr := m.backups.FinishBackup(finishCtx, finished); ferr != nil {
		m.logger.Error("backups: could not record the backup outcome",
			"backup_id", backup.ID.String(), "error", ferr)
		return
	}
	m.notifyBackup(finishCtx, finished, resource)
}

func (m *BackupManager) runRestore(restore Restore, backup Backup, resource Resource, source Source) {
	defer m.release(resource.ID)

	ctx, cancel := context.WithTimeout(context.Background(), m.jobTimeout)
	defer cancel()

	err := m.restore(ctx, source, resource, backup)
	finished := restore
	finished.FinishedAt = m.now()
	if err != nil {
		finished.Status = RestoreFailed
		finished.Error = boundedDiag(err.Error())
		m.logger.Error("backups: restore failed",
			"restore_id", restore.ID.String(), "backup_id", backup.ID.String(),
			"resource", resource.Ref().String(), "error", err)
	} else {
		finished.Status = RestoreCompleted
		m.logger.Info("backups: restore completed",
			"restore_id", restore.ID.String(), "backup_id", backup.ID.String(),
			"resource", resource.Ref().String())
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishCancel()
	if _, ferr := m.backups.FinishRestore(finishCtx, finished); ferr != nil {
		m.logger.Error("backups: could not record the restore outcome",
			"restore_id", restore.ID.String(), "error", ferr)
	}
}

func (m *BackupManager) capture(
	ctx context.Context,
	source Source,
	resource Resource,
	target *BackupTarget,
	runID uuid.UUID,
	pause bool,
) (string, int64, string, error) {
	temp, err := os.CreateTemp("", "gotham-backup-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("backups: create staging file: %w", err)
	}
	defer func() {
		name := temp.Name()
		_ = temp.Close()
		_ = os.Remove(name)
	}()
	compressor := gzip.NewWriter(temp)
	result, jobErr := source.Capture(ctx, CaptureRequest{
		Resource: resource,
		RunID:    runID,
		Sink:     compressor,
		Recorder: runRecorder{repo: m.backups},
		Pause:    pause,
	})
	if closeErr := compressor.Close(); closeErr != nil && jobErr == nil {
		jobErr = fmt.Errorf("backups: finish compression: %w", closeErr)
	}
	if syncErr := temp.Sync(); syncErr != nil && jobErr == nil {
		jobErr = fmt.Errorf("backups: flush staging file: %w", syncErr)
	}
	if jobErr != nil {
		return "", 0, result.ContainerID, jobErr
	}
	info, statErr := temp.Stat()
	if statErr != nil {
		return "", 0, result.ContainerID, fmt.Errorf("backups: stat staging file: %w", statErr)
	}
	size := info.Size()
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return "", 0, result.ContainerID, fmt.Errorf("backups: rewind staging file: %w", err)
	}
	store, key, err := m.storeAndKey(ctx, target, resource, runID, result.Extension)
	if err != nil {
		return "", 0, result.ContainerID, err
	}
	location, err := store.Put(ctx, key, temp, size)
	if err != nil {
		return "", 0, result.ContainerID, err
	}
	return location, size, result.ContainerID, nil
}

func (m *BackupManager) restore(ctx context.Context, source Source, resource Resource, backup Backup) error {
	store, err := m.storeForLocation(ctx, backup, resource.OwnerID)
	if err != nil {
		return err
	}
	artifact, err := store.Get(ctx, backup.Location)
	if err != nil {
		return err
	}
	defer func() { _ = artifact.Close() }()
	return source.Restore(ctx, RestoreRequest{
		Resource: resource,
		BackupID: backup.ID,
		Artifact: artifact,
	})
}

// runRecorder persists the observations a driver makes while its job runs.
type runRecorder struct {
	repo BackupRepository
}

func (r runRecorder) SetWasRunning(ctx context.Context, runID uuid.UUID, running bool) error {
	if r.repo == nil {
		return nil
	}
	return r.repo.SetBackupWasRunning(ctx, runID, running)
}

type backupServerLister interface {
	ListServerIDs(ctx context.Context) ([]uuid.UUID, error)
}

// sweepJobContainers removes temporary job containers a previous control plane
// left behind. Labels are the shared contract between the core and a driver;
// the sweep never touches a container a live run still owns.
func (m *BackupManager) sweepJobContainers() {
	if m == nil || m.containers == nil || m.backups == nil {
		return
	}
	lister, ok := m.backups.(backupServerLister)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	serverIDs, err := lister.ListServerIDs(ctx)
	if err != nil {
		m.logger.Warn("backups: container sweep could not list servers", "error", err)
		return
	}
	removed := 0
	for _, serverID := range serverIDs {
		list, err := m.listContainersFresh(ctx, serverID)
		if err != nil {
			m.logger.Warn("backups: container sweep could not list node containers",
				"server_id", serverID.String(), "error", err)
			continue
		}
		liveRuns, leased := m.liveGuards(ctx)
		if liveRuns == nil || leased == nil {
			continue
		}
		for _, container := range list {
			if !isStaleJobContainer(container, leased, liveRuns) {
				continue
			}
			m.removeJobContainer(serverID, container.ID)
			removed++
		}
	}
	if removed > 0 {
		m.logger.Info("backups: removed leftover backup job containers", "count", removed)
	}
}

func (m *BackupManager) liveGuards(ctx context.Context) (liveRuns, leased map[uuid.UUID]bool) {
	liveRuns = map[uuid.UUID]bool{}
	leased = map[uuid.UUID]bool{}
	running, err := m.backups.ListRunningBackups(ctx)
	if err != nil {
		m.logger.Warn("backups: container sweep could not list running backups", "error", err)
		return nil, nil
	}
	for _, backup := range running {
		liveRuns[backup.ID] = true
	}
	for _, resourceID := range m.leases.HeldIDs() {
		leased[resourceID] = true
	}
	return liveRuns, leased
}

func (m *BackupManager) listContainersFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error) {
	type freshLister interface {
		ListFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error)
	}
	if lister, ok := m.containers.(freshLister); ok {
		return lister.ListFresh(ctx, serverID)
	}
	return m.containers.List(ctx, serverID)
}

func (m *BackupManager) removeJobContainer(serverID uuid.UUID, containerID string) {
	if containerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.containers.Remove(ctx, serverID, containerID); err != nil &&
		!errors.Is(err, containers.ErrContainerNotFound) {
		m.logger.Warn("backups: could not remove the job container",
			"container_id", containerID, "error", err)
	}
}

func isStaleJobContainer(c containers.Container, leased, liveRuns map[uuid.UUID]bool) bool {
	if c.Labels[LabelManaged] != "true" {
		return false
	}
	switch c.Labels[LabelRole] {
	case RoleBackup, RoleRestore, RoleStage:
	default:
		return false
	}
	if id, err := uuid.Parse(c.Labels[LabelResourceID]); err == nil && leased[id] {
		return false
	}
	if id, err := uuid.Parse(c.Labels[LabelBackupID]); err == nil && liveRuns[id] {
		return false
	}
	return true
}
