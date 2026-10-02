package databases

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// Job execution: the temporary-container flow of dump and restore jobs.

// startRun records a backup row, claims the database's single-flight slot and
// spawns the job. It is shared by the HTTP surface and the scheduler.
func (m *BackupManager) startRun(ctx context.Context, database Database, target *BackupTarget, typ BackupType, scheduleID uuid.UUID) (Backup, error) {
	if err := m.backupsReady(); err != nil {
		return Backup{}, err
	}
	// Ownership must be consistent end to end. A target is user-global, while
	// a database can belong to a team: a member could otherwise back up a
	// database they share to their own S3 target, and the read path — which
	// resolves the target by the database owner — would then 404 on every
	// restore. An S3 target is therefore usable only when its owner is the
	// database owner (a member's own local target is still fine: it records a
	// file:// location that needs no target to read back).
	if target != nil && target.Kind == TargetS3 && target.UserID != database.UserID {
		return Backup{}, ErrNotFound
	}
	if !m.claim(database.ID) {
		return Backup{}, ErrBackupInFlight
	}
	backup := Backup{
		ID:         uuid.New(),
		DatabaseID: database.ID,
		ScheduleID: scheduleID,
		Type:       typ,
		Status:     BackupRunning,
		CreatedAt:  m.now(),
	}
	if target != nil {
		backup.TargetID = target.ID
	}
	stored, liveTarget, err := m.backups.CreateBackupWithTarget(ctx, backup)
	if err != nil {
		m.release(database.ID)
		return Backup{}, err
	}
	// liveTarget is the target read under the same row lock as the run insert,
	// so a concurrent destination edit cannot strand this run.
	go m.runBackup(stored, database, liveTarget)
	return stored, nil
}

// runBackup is the whole dump job: stop the database, dump it from a
// temporary container, compress and store the bytes, then put the database
// back the way it was. Any failure is recorded on the row.
func (m *BackupManager) runBackup(backup Backup, database Database, target *BackupTarget) {
	defer m.release(database.ID)

	ctx, cancel := context.WithTimeout(context.Background(), m.jobTimeout)
	defer cancel()

	location, size, containerID, err := m.dump(ctx, database, target, backup.ID)
	finished := backup
	finished.ContainerID = containerID
	finished.FinishedAt = m.now()
	if err != nil {
		finished.Status = BackupFailed
		finished.Error = boundedDiag(err.Error())
		m.logger.Error("databases: backup failed",
			"backup_id", backup.ID.String(), "database_id", database.ID.String(),
			"type", string(backup.Type), "error", err)
	} else {
		finished.Status = BackupCompleted
		finished.Location = location
		finished.Size = size
		m.logger.Info("databases: backup completed",
			"backup_id", backup.ID.String(), "database_id", database.ID.String(),
			"type", string(backup.Type), "size", size, "location", location)
	}

	// The job context may already be expired; the row write gets its own.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishCancel()
	if _, ferr := m.backups.FinishBackup(finishCtx, finished); ferr != nil {
		m.logger.Error("databases: could not record the backup outcome",
			"backup_id", backup.ID.String(), "error", ferr)
		return
	}
	m.notifyBackup(finishCtx, finished, database)
}

// runRestore is the restore job: download the artifact, stage it onto the
// database volume in chunks and apply it from a temporary container.
func (m *BackupManager) runRestore(backup Backup, database Database) {
	defer m.release(database.ID)

	ctx, cancel := context.WithTimeout(context.Background(), m.jobTimeout)
	defer cancel()

	if err := m.restore(ctx, database, backup); err != nil {
		m.logger.Error("databases: restore failed",
			"backup_id", backup.ID.String(), "database_id", database.ID.String(), "error", err)
		return
	}
	m.logger.Info("databases: restore completed",
		"backup_id", backup.ID.String(), "database_id", database.ID.String())
}

// dump stops the database, runs the engine's dump job on the node, compresses
// the payload into a temporary file and uploads it to the target. It returns
// the artifact location, its compressed size and the temporary container id.
func (m *BackupManager) dump(ctx context.Context, database Database, target *BackupTarget, runID uuid.UUID) (string, int64, string, error) {
	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return "", 0, "", fmt.Errorf("%w: engine %q has no backup engine", ErrValidation, database.Engine)
	}
	credentials, err := m.databaseCredentials(ctx, database)
	if err != nil {
		return "", 0, "", err
	}
	wasRunning, err := m.pauseDatabase(ctx, database)
	if err != nil {
		return "", 0, "", err
	}
	if wasRunning {
		defer func() {
			if resumeErr := m.resumeDatabase(context.Background(), database); resumeErr != nil {
				m.logger.Error("databases: could not restart the database after the backup",
					"database_id", database.ID.String(), "error", resumeErr)
			}
		}()
	}

	options, err := engine.DumpOptions(database, credentials, runID.String())
	if err != nil {
		return "", 0, "", err
	}

	temp, err := os.CreateTemp("", "gotham-backup-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("databases: create staging file: %w", err)
	}
	defer func() {
		name := temp.Name()
		_ = temp.Close()
		_ = os.Remove(name)
	}()
	compressor := gzip.NewWriter(temp)
	collector := newJobCollector(runID.String(), compressor)

	containerID, jobErr := m.runJob(ctx, database.ServerID, options, collector)
	if jobErr == nil {
		jobErr = collector.Result()
	}
	if closeErr := compressor.Close(); closeErr != nil && jobErr == nil {
		jobErr = fmt.Errorf("databases: finish compression: %w", closeErr)
	}
	if syncErr := temp.Sync(); syncErr != nil && jobErr == nil {
		jobErr = fmt.Errorf("databases: flush staging file: %w", syncErr)
	}
	if jobErr != nil {
		return "", 0, containerID, jobErr
	}
	// The stored artifact is the compressed stream, so the size that matters
	// is the file's — the collector counted the raw payload it decoded.
	info, statErr := temp.Stat()
	if statErr != nil {
		return "", 0, containerID, fmt.Errorf("databases: stat staging file: %w", statErr)
	}
	size := info.Size()
	// Re-read the compressed bytes: the file was written sequentially.
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return "", 0, containerID, fmt.Errorf("databases: rewind staging file: %w", err)
	}
	store, key, err := m.storeAndKey(ctx, target, database, runID)
	if err != nil {
		return "", 0, containerID, err
	}
	location, err := store.Put(ctx, key, temp, size)
	if err != nil {
		return "", 0, containerID, err
	}
	return location, size, containerID, nil
}

// restore downloads the artifact, stages it onto the volume in bounded
// chunks and runs the engine's restore job, with the database stopped
// throughout so the temporary container is the only writer.
func (m *BackupManager) restore(ctx context.Context, database Database, backup Backup) error {
	store, err := m.storeForLocation(ctx, backup, &database)
	if err != nil {
		return err
	}
	artifact, err := store.Get(ctx, backup.Location)
	if err != nil {
		return err
	}
	defer func() { _ = artifact.Close() }()

	wasRunning, err := m.pauseDatabase(ctx, database)
	if err != nil {
		return err
	}
	if wasRunning {
		defer func() {
			if resumeErr := m.resumeDatabase(context.Background(), database); resumeErr != nil {
				m.logger.Error("databases: could not restart the database after the restore",
					"database_id", database.ID.String(), "error", resumeErr)
			}
		}()
	}

	staged, err := stagingPath(database, backup.ID)
	if err != nil {
		return err
	}
	if err := m.stageArtifact(ctx, database, backup.ID, artifact, staged); err != nil {
		m.removeStagedArtifact(database, staged)
		return err
	}
	// Any later failure leaves a staged artifact behind in the volume; a
	// best-effort cleanup keeps a retry from starting on a partial file.
	restoreSucceeded := false
	defer func() {
		if !restoreSucceeded {
			m.removeStagedArtifact(database, staged)
		}
	}()

	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return fmt.Errorf("%w: engine %q has no backup engine", ErrValidation, database.Engine)
	}
	credentials, err := m.databaseCredentials(ctx, database)
	if err != nil {
		return err
	}
	options, err := engine.RestoreOptions(database, credentials, backup.ID.String(), staged)
	if err != nil {
		return err
	}
	collector := newJobCollector(backup.ID.String(), nil)
	if _, err := m.runJob(ctx, database.ServerID, options, collector); err != nil {
		return err
	}
	if err := collector.Result(); err != nil {
		return err
	}
	restoreSucceeded = true
	return nil
}

// removeStagedArtifact best-effort removes a staged restore artifact and its
// directory after a failed restore, so a retry starts from a clean volume. It
// runs its own bounded job with a fresh context — the restore's may already be
// gone — and every failure is logged, never returned: the caller must keep
// the original restore error.
func (m *BackupManager) removeStagedArtifact(database Database, stagedPath string) {
	engine, _, err := parseEngine(database.Engine)
	if err != nil || !safeJobPath(stagedPath) {
		return
	}
	volume := engine.VolumeSpec()
	if strings.TrimSpace(volume.MountPath) == "" {
		return
	}
	runID := database.ID.String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// The script reports through the standard frame, so the collector
	// validates that the removal actually ran.
	script := fmt.Sprintf(
		"if rm -f '%s' && rm -rf '%s'; then\n"+
			"  printf 'GOTHAM-BACKUP-START %%s\\nGOTHAM-BACKUP-PAYLOAD %%s 0\\nGOTHAM-BACKUP-END %%s ok\\n' \"$GOTHAM_RUN_ID\" \"$GOTHAM_RUN_ID\" \"$GOTHAM_RUN_ID\"\n"+
			"else\n"+
			"  printf 'GOTHAM-BACKUP-START %%s\\nGOTHAM-BACKUP-END %%s fail 3\\n' \"$GOTHAM_RUN_ID\" \"$GOTHAM_RUN_ID\"\n"+
			"fi\n",
		stagedPath, path.Dir(stagedPath))
	options := containers.RunOptions{
		Image:   engine.Image(database.Version),
		Name:    tempJobName(roleStage+"-cleanup", runID),
		Env:     []string{"GOTHAM_RUN_ID=" + runID},
		Command: []string{"sh", "-c", script},
		Labels: map[string]string{
			labelManaged:    "true",
			labelDatabaseID: runID,
			labelEngine:     database.Engine,
			labelRole:       roleStage,
			labelBackupID:   runID,
		},
		Volumes: []string{database.StoragePath + ":" + volume.MountPath},
	}
	collector := newJobCollector(runID, nil)
	if _, err := m.runJob(ctx, database.ServerID, options, collector); err != nil {
		m.logger.Warn("databases: could not clean up a staged restore artifact",
			"database_id", runID, "error", err)
		return
	}
	if err := collector.Result(); err != nil {
		m.logger.Warn("databases: staged restore artifact cleanup did not complete",
			"database_id", runID, "error", err)
	}
}

// stageArtifact writes the artifact onto the database volume one bounded
// chunk at a time. Each chunk is a temporary container whose command carries
// a base64 payload: the agent contract has no file-transfer RPC, and a
// command argument is the largest channel that survives it. The binding
// limit is Linux's per-argument MAX_ARG_STRLEN (128 KiB), not ARG_MAX.
func (m *BackupManager) stageArtifact(ctx context.Context, database Database, runID uuid.UUID, data io.Reader, stagedPath string) error {
	buffer := make([]byte, stageChunkBytes)
	for index := 0; ; index++ {
		n, err := io.ReadFull(data, buffer)
		if n > 0 {
			if stageErr := m.stageChunk(ctx, database, runID, buffer[:n], stagedPath, index); stageErr != nil {
				return stageErr
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil
		default:
			return fmt.Errorf("databases: read backup artifact: %w", err)
		}
	}
}

// stageChunk appends (or, for index 0, replaces) one chunk of the artifact
// inside the database volume and waits for the container to finish. The chunk
// travels base64-encoded inside the command argument — the only channel the
// agent contract offers — and is decoded back to the artifact bytes by the
// container, so the restore job reads the raw gzip file it expects.
func (m *BackupManager) stageChunk(ctx context.Context, database Database, runID uuid.UUID, chunk []byte, stagedPath string, index int) error {
	encoded := base64.StdEncoding.EncodeToString(chunk)
	operator := ">>"
	if index == 0 {
		operator = ">"
	}
	directory := path.Dir(stagedPath)
	if !safeJobPath(directory) || !safeJobPath(stagedPath) {
		return fmt.Errorf("%w: staging path contains characters a shell argument cannot carry", ErrValidation)
	}
	// The chunk never contains shell metacharacters: base64 is alphanumerics
	// plus "+" "/" "=", and both paths are built from a validated mount path
	// and a UUID.
	script := fmt.Sprintf(
		"id=\"$GOTHAM_RUN_ID\"\n"+
			"if mkdir -p '%s' && printf '%%s' '%s' | base64 -d %s '%s'; then\n"+
			"  printf 'GOTHAM-BACKUP-START %%s\\nGOTHAM-BACKUP-PAYLOAD %%s 0\\nGOTHAM-BACKUP-END %%s ok\\n' \"$id\" \"$id\" \"$id\"\n"+
			"else\n"+
			"  printf 'GOTHAM-BACKUP-START %%s\\nGOTHAM-BACKUP-END %%s fail 3\\n' \"$id\" \"$id\"\n"+
			"fi\n",
		directory, encoded, operator, stagedPath)

	engine, _, err := parseEngine(database.Engine)
	if err != nil {
		return err
	}
	options := containers.RunOptions{
		Image:   engine.Image(database.Version),
		Name:    tempJobName(roleStage+"-"+strconv.Itoa(index), runID.String()),
		Env:     []string{"GOTHAM_RUN_ID=" + runID.String()},
		Command: []string{"sh", "-c", script},
		Labels: map[string]string{
			labelManaged:    "true",
			labelDatabaseID: database.ID.String(),
			labelEngine:     database.Engine,
			labelRole:       roleStage,
			labelBackupID:   runID.String(),
		},
		Volumes: []string{database.StoragePath + ":" + engine.VolumeSpec().MountPath},
	}
	collector := newJobCollector(runID.String(), nil)
	if _, err := m.runJob(ctx, database.ServerID, options, collector); err != nil {
		return err
	}
	return collector.Result()
}

// runJob starts a temporary container, feeds its log stream into sink and
// returns the container id. The container is removed afterwards, success or
// not: a job container exists only while its job runs.
func (m *BackupManager) runJob(ctx context.Context, serverID uuid.UUID, options containers.RunOptions, sink io.Writer) (string, error) {
	containerID, err := m.containers.Run(ctx, serverID, options)
	if err != nil {
		return "", mapContainerError(err)
	}
	defer m.removeJobContainer(serverID, containerID)

	logs, streamErr, err := m.containers.Logs(ctx, serverID, containerID, true)
	if err != nil {
		return containerID, mapContainerError(err)
	}
	// The channel closes when Docker ends the stream — with follow, that is
	// when the container exits — or when the job context expires.
	for chunk := range logs {
		if sink != nil {
			// jobCollector records write failures internally, so a failed
			// write must not stop the drain: the channel has to be emptied
			// for the agent goroutine to finish.
			_, _ = sink.Write(chunk)
		}
	}
	// A terminal stream error means the dump may be truncated: report it
	// instead of treating the job as a clean success.
	if err := <-streamErr; err != nil {
		return containerID, mapContainerError(err)
	}
	return containerID, ctx.Err()
}

// removeJobContainer force-removes a temporary container with its own
// context: the job's context may already be gone, and a leaked container
// would hold the volume's data directory.
func (m *BackupManager) removeJobContainer(serverID uuid.UUID, containerID string) {
	if containerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.containers.Remove(ctx, serverID, containerID); err != nil && !errors.Is(err, containers.ErrContainerNotFound) {
		m.logger.Warn("databases: could not remove the job container",
			"container_id", containerID, "error", err)
	}
}

// backupServerLister is the slice of the repository the startup container
// sweep needs: every managed node id. It is separate from BackupRepository so
// the backup persistence contract stays about backup tables.
type backupServerLister interface {
	ListServerIDs(ctx context.Context) ([]uuid.UUID, error)
}

// sweepJobContainers removes temporary job containers a crashed control plane
// left behind. It runs once at construction, after the stale-run sweep: a
// container is removed only when neither its database is leased by a live job
// nor its run is still running, so a job this process is actively driving is
// never killed.
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

	liveRuns := map[uuid.UUID]bool{}
	running, err := m.backups.ListRunningBackups(ctx)
	if err != nil {
		m.logger.Warn("databases: container sweep could not list running backups", "error", err)
		return
	}
	for _, backup := range running {
		liveRuns[backup.ID] = true
	}

	leased := map[uuid.UUID]bool{}
	m.mu.Lock()
	for id := range m.inflight {
		leased[id] = true
	}
	m.mu.Unlock()

	serverIDs, err := lister.ListServerIDs(ctx)
	if err != nil {
		m.logger.Warn("databases: container sweep could not list servers", "error", err)
		return
	}
	removed := 0
	for _, serverID := range serverIDs {
		list, err := m.listContainersFresh(ctx, serverID)
		if err != nil {
			m.logger.Warn("databases: container sweep could not list node containers",
				"server_id", serverID.String(), "error", err)
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
		m.logger.Info("databases: removed leftover backup job containers", "count", removed)
	}
}

// listContainersFresh reads a node's containers straight from the agent when
// the service supports it. The startup sweep must not read the List cache: a
// hit serves an earlier snapshot (and an older binary cached containers whose
// labels were dropped), so a crash's leftover job container could be missed.
func (m *BackupManager) listContainersFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error) {
	type freshLister interface {
		ListFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error)
	}
	if lister, ok := m.containers.(freshLister); ok {
		return lister.ListFresh(ctx, serverID)
	}
	return m.containers.List(ctx, serverID)
}

// isStaleJobContainer reports whether c is a temporary backup job container
// the sweep may remove. Containers of another role (the database container
// itself) and containers still leased by a live run are never selected.
func isStaleJobContainer(c containers.Container, leased, liveRuns map[uuid.UUID]bool) bool {
	if c.Labels[labelManaged] != "true" {
		return false
	}
	switch c.Labels[labelRole] {
	case roleBackup, roleRestore, roleStage:
	default:
		return false
	}
	if id, err := uuid.Parse(c.Labels[labelDatabaseID]); err == nil && leased[id] {
		return false
	}
	if id, err := uuid.Parse(c.Labels[labelBackupID]); err == nil && liveRuns[id] {
		return false
	}
	return true
}

// pauseDatabase stops the database container before a job mounts its volume,
// reporting whether it was running. A database without a container (row
// created, container gone) needs no pause.
func (m *BackupManager) pauseDatabase(ctx context.Context, database Database) (bool, error) {
	if database.ContainerID == "" {
		return false, nil
	}
	list, err := m.containers.List(ctx, database.ServerID)
	if err != nil {
		return false, mapContainerError(err)
	}
	running := false
	for _, item := range list {
		if item.ID == database.ContainerID {
			running = item.State == "running"
			break
		}
	}
	if !running {
		return false, nil
	}
	if err := m.containers.Stop(ctx, database.ServerID, database.ContainerID); err != nil {
		return false, mapContainerError(err)
	}
	return true, nil
}

// resumeDatabase starts the database container again. It runs with a fresh
// context so a cancelled job still brings the database back up.
func (m *BackupManager) resumeDatabase(ctx context.Context, database Database) error {
	if database.ContainerID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := m.containers.Start(ctx, database.ServerID, database.ContainerID); err != nil {
		return mapContainerError(err)
	}
	return nil
}
