package databases

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/backups"
	"github.com/justindeelux/gotham/internal/containers"
)

// BackupSource is the database driver of the backup core: it resolves a
// database, captures an engine dump into the core's sink and applies an
// artifact back to the database volume. Providers, schedules, runs, restores
// and the job lifecycle stay in the core.
type BackupSource struct {
	svc *Service
}

var _ backups.Source = (*BackupSource)(nil)

func NewBackupSource(svc *Service) *BackupSource {
	return &BackupSource{svc: svc}
}

func (s *BackupSource) Resolve(ctx context.Context, ref backups.ResourceRef) (backups.Resource, error) {
	database, err := s.database(ctx, ref)
	if err != nil {
		return backups.Resource{}, err
	}
	return backups.Resource{
		Type:    backups.ResourceDatabase,
		ID:      database.ID,
		OwnerID: database.UserID,
		Name:    database.Name,
	}, nil
}

func (s *BackupSource) Describe(ctx context.Context, ref backups.ResourceRef) (string, error) {
	database, err := s.database(ctx, ref)
	if err != nil {
		return "", err
	}
	return database.Name, nil
}

func (s *BackupSource) Capture(ctx context.Context, req backups.CaptureRequest) (backups.CaptureResult, error) {
	database, err := s.database(ctx, req.Resource.Ref())
	if err != nil {
		return backups.CaptureResult{}, err
	}
	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return backups.CaptureResult{}, fmt.Errorf("%w: engine %q has no backup engine",
			backups.ErrValidation, database.Engine)
	}
	credentials, err := s.credentials(ctx, database)
	if err != nil {
		return backups.CaptureResult{}, err
	}
	result := backups.CaptureResult{Extension: engine.DumpExtension()}
	wasRunning, err := s.databaseRunning(ctx, database)
	if err != nil {
		return result, err
	}
	if wasRunning {
		defer func() {
			if resumeErr := s.resumeDatabase(context.Background(), database); resumeErr != nil {
				s.svc.logger.Error("databases: could not restart the database after the backup",
					"database_id", database.ID.String(), "error", resumeErr)
			}
		}()
	}
	if req.Recorder != nil {
		if recordErr := req.Recorder.SetWasRunning(ctx, req.RunID, wasRunning); recordErr != nil {
			return result, fmt.Errorf("databases: record the pre-backup database state: %w", recordErr)
		}
	}
	if wasRunning {
		if err := s.stopDatabase(ctx, database); err != nil {
			return result, err
		}
	}

	options, err := engine.DumpOptions(database, credentials, req.RunID.String())
	if err != nil {
		return result, err
	}
	collector := newJobCollector(req.RunID.String(), req.Sink)
	containerID, jobErr := s.runJob(ctx, database.ServerID, options, collector)
	result.ContainerID = containerID
	if jobErr == nil {
		jobErr = collector.Result()
	}
	return result, jobErr
}

func (s *BackupSource) Restore(ctx context.Context, req backups.RestoreRequest) error {
	database, err := s.database(ctx, req.Resource.Ref())
	if err != nil {
		return err
	}
	wasRunning, err := s.pauseDatabase(ctx, database)
	if err != nil {
		return err
	}
	if err := s.applyRestore(ctx, database, req.BackupID, req.Artifact); err != nil {
		s.svc.markError(context.Background(), database, err, Credentials{})
		return err
	}
	if wasRunning {
		if resumeErr := s.resumeDatabase(context.Background(), database); resumeErr != nil {
			s.svc.logger.Error("databases: could not restart the database after the restore",
				"database_id", database.ID.String(), "error", resumeErr)
			s.svc.markError(context.Background(), database, resumeErr, Credentials{})
		}
	}
	return nil
}

func (s *BackupSource) Recover(ctx context.Context, req backups.RecoverRequest) error {
	database, err := s.database(ctx, req.Resource.Ref())
	if err != nil {
		if errors.Is(err, backups.ErrNotFound) {
			return nil
		}
		return err
	}
	s.cleanupOrphanJobs(ctx, database)
	if req.WasRestore {
		if staged, pathErr := stagingPath(database, req.BackupID); pathErr == nil {
			s.removeStagedArtifact(database, staged)
		}
		s.svc.markError(ctx, database, errors.New("control plane restarted during the restore"), Credentials{})
		return nil
	}
	if req.WasRunning {
		return s.resumeDatabase(ctx, database)
	}
	return nil
}

func (s *BackupSource) database(ctx context.Context, ref backups.ResourceRef) (Database, error) {
	if ref.Type != backups.ResourceDatabase {
		return Database{}, fmt.Errorf("%w: %s", backups.ErrUnsupportedResource, ref.Type)
	}
	if ref.ID == uuid.Nil {
		return Database{}, fmt.Errorf("%w: database id is required", backups.ErrValidation)
	}
	database, err := s.svc.repo.GetDatabase(ctx, ref.ID)
	if err != nil {
		return Database{}, mapBackupError(err)
	}
	return database, nil
}

// mapBackupError translates the database repository errors the core understands
// so an alias and the generic surface answer the same status codes.
func mapBackupError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return fmt.Errorf("%w: %v", backups.ErrNotFound, err)
	case errors.Is(err, ErrValidation):
		return fmt.Errorf("%w: %v", backups.ErrValidation, err)
	default:
		return err
	}
}

func (s *BackupSource) credentials(ctx context.Context, database Database) (Credentials, error) {
	secrets, err := s.svc.repo.ListSecrets(ctx, database.ID)
	if err != nil {
		return Credentials{}, err
	}
	return openCredentials(s.svc.secret, secrets)
}

func (s *BackupSource) applyRestore(ctx context.Context, database Database, backupID uuid.UUID, artifact io.Reader) error {
	staged, err := stagingPath(database, backupID)
	if err != nil {
		return err
	}
	if err := s.stageArtifact(ctx, database, backupID, artifact, staged); err != nil {
		s.removeStagedArtifact(database, staged)
		return err
	}
	restoreSucceeded := false
	defer func() {
		if !restoreSucceeded {
			s.removeStagedArtifact(database, staged)
		}
	}()

	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return fmt.Errorf("%w: engine %q has no backup engine", backups.ErrValidation, database.Engine)
	}
	credentials, err := s.credentials(ctx, database)
	if err != nil {
		return err
	}
	options, err := engine.RestoreOptions(database, credentials, backupID.String(), staged)
	if err != nil {
		return err
	}
	collector := newJobCollector(backupID.String(), nil)
	if _, err := s.runJob(ctx, database.ServerID, options, collector); err != nil {
		return err
	}
	if err := collector.Result(); err != nil {
		return err
	}
	restoreSucceeded = true
	return nil
}

func (s *BackupSource) removeStagedArtifact(database Database, stagedPath string) {
	engine, _, err := parseEngine(database.Engine)
	if err != nil || !safeJobPath(stagedPath) {
		return
	}
	volume := volumeSpecFor(engine, database.Version)
	if strings.TrimSpace(volume.MountPath) == "" {
		return
	}
	runID := database.ID.String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
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
	if _, err := s.runJob(ctx, database.ServerID, options, collector); err != nil {
		s.svc.logger.Warn("databases: could not clean up a staged restore artifact",
			"database_id", runID, "error", err)
		return
	}
	if err := collector.Result(); err != nil {
		s.svc.logger.Warn("databases: staged restore artifact cleanup did not complete",
			"database_id", runID, "error", err)
	}
}

func (s *BackupSource) stageArtifact(ctx context.Context, database Database, runID uuid.UUID, data io.Reader, stagedPath string) error {
	buffer := make([]byte, stageChunkBytes)
	for index := 0; ; index++ {
		n, err := io.ReadFull(data, buffer)
		if n > 0 {
			if stageErr := s.stageChunk(ctx, database, runID, buffer[:n], stagedPath, index); stageErr != nil {
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

func (s *BackupSource) stageChunk(ctx context.Context, database Database, runID uuid.UUID, chunk []byte, stagedPath string, index int) error {
	encoded := base64.StdEncoding.EncodeToString(chunk)
	operator := ">>"
	if index == 0 {
		operator = ">"
	}
	directory := path.Dir(stagedPath)
	if !safeJobPath(directory) || !safeJobPath(stagedPath) {
		return fmt.Errorf("%w: staging path contains characters a shell argument cannot carry", backups.ErrValidation)
	}
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
		Volumes: []string{database.StoragePath + ":" + volumeSpecFor(engine, database.Version).MountPath},
	}
	collector := newJobCollector(runID.String(), nil)
	if _, err := s.runJob(ctx, database.ServerID, options, collector); err != nil {
		return err
	}
	return collector.Result()
}

func (s *BackupSource) runJob(ctx context.Context, serverID uuid.UUID, options containers.RunOptions, sink io.Writer) (string, error) {
	containerID, err := s.svc.containers.Run(ctx, serverID, options)
	if err != nil {
		return "", mapContainerError(err)
	}
	defer s.removeJobContainer(serverID, containerID)

	logs, streamErr, err := s.svc.containers.Logs(ctx, serverID, containerID, true)
	if err != nil {
		return containerID, mapContainerError(err)
	}
	for chunk := range logs {
		if sink != nil {
			_, _ = sink.Write(chunk)
		}
	}
	if err := <-streamErr; err != nil {
		return containerID, mapContainerError(err)
	}
	return containerID, ctx.Err()
}

func (s *BackupSource) removeJobContainer(serverID uuid.UUID, containerID string) {
	if containerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.svc.containers.Remove(ctx, serverID, containerID); err != nil &&
		!errors.Is(err, containers.ErrContainerNotFound) {
		s.svc.logger.Warn("databases: could not remove the job container",
			"container_id", containerID, "error", err)
	}
}

func (s *BackupSource) cleanupOrphanJobs(ctx context.Context, database Database) {
	if s.svc.containers == nil || database.ServerID == uuid.Nil {
		return
	}
	list, err := s.svc.containers.List(ctx, database.ServerID)
	if err != nil {
		s.svc.logger.Warn("databases: could not list job containers for recovery",
			"database_id", database.ID.String(), "error", err)
		return
	}
	for _, item := range list {
		if !isOrphanJobContainer(item, database.ID) {
			continue
		}
		if err := s.svc.containers.Remove(ctx, database.ServerID, item.ID); err != nil &&
			!errors.Is(err, containers.ErrContainerNotFound) {
			s.svc.logger.Warn("databases: could not remove an orphan job container",
				"database_id", database.ID.String(), "container_id", item.ID, "error", err)
		}
	}
}

func isOrphanJobContainer(item containers.Container, databaseID uuid.UUID) bool {
	if item.Labels[labelManaged] != "true" || item.Labels[labelDatabaseID] != databaseID.String() {
		return false
	}
	switch item.Labels[labelRole] {
	case roleBackup, roleRestore, roleStage:
		return true
	default:
		return false
	}
}

func (s *BackupSource) databaseRunning(ctx context.Context, database Database) (bool, error) {
	if database.ContainerID == "" {
		return false, nil
	}
	list, err := s.svc.containers.List(ctx, database.ServerID)
	if err != nil {
		return false, mapContainerError(err)
	}
	for _, item := range list {
		if item.ID == database.ContainerID {
			return item.State == "running", nil
		}
	}
	return false, nil
}

func (s *BackupSource) stopDatabase(ctx context.Context, database Database) error {
	if database.ContainerID == "" {
		return nil
	}
	if err := s.svc.containers.Stop(ctx, database.ServerID, database.ContainerID); err != nil {
		return mapContainerError(err)
	}
	return nil
}

func (s *BackupSource) pauseDatabase(ctx context.Context, database Database) (bool, error) {
	running, err := s.databaseRunning(ctx, database)
	if err != nil || !running {
		return false, err
	}
	if err := s.stopDatabase(ctx, database); err != nil {
		return false, err
	}
	return true, nil
}

func (s *BackupSource) resumeDatabase(ctx context.Context, database Database) error {
	if database.ContainerID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := s.svc.containers.Start(ctx, database.ServerID, database.ContainerID); err != nil {
		return mapContainerError(err)
	}
	return nil
}
