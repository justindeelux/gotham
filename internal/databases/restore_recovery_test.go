package databases

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func failingRestoreLogs(opts containers.RunOptions) [][]byte {
	runID := envValue(opts.Env, "GOTHAM_RUN_ID")
	if opts.Labels[labelRole] == roleRestore {
		return [][]byte{[]byte(
			jobStartPrefix + runID + "\n" +
				jobEndPrefix + runID + " fail 1\n" +
				"engine: restore died midway\n")}
	}
	return defaultJobLogs(opts)
}

func TestFailedRestoreLeavesDatabaseStopped(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)

	fixture.containers.setRunning(fixture.database.ContainerID)

	fixture.containers.mu.Lock()
	startsBefore := fixture.containers.starts
	fixture.containers.logFn = failingRestoreLogs
	fixture.containers.mu.Unlock()

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: backup.ID})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if result.RestoreID == uuid.Nil {
		t.Fatal("RestoreBackup returned no restore id")
	}
	waitFor(t, "the restore to fail", func() bool {
		restore, ok := fixture.backups.getRestore(result.RestoreID)
		return ok && restore.Status != RestoreRunning
	})

	restore, _ := fixture.backups.getRestore(result.RestoreID)
	if restore.Status != RestoreFailed {
		t.Errorf("restore status = %q, want %q", restore.Status, RestoreFailed)
	}
	if restore.Error == "" {
		t.Error("failed restore carries no error")
	}

	fixture.containers.mu.Lock()
	starts, stops := fixture.containers.starts, fixture.containers.stops
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	fixture.containers.mu.Unlock()
	if starts != startsBefore {
		t.Errorf("starts = %d, want %d: the database was restarted into a partial restore", starts, startsBefore)
	}
	if stops <= 1 {
		t.Errorf("stops = %d, want the restore to have paused the database", stops)
	}

	cleanups := 0
	for _, opts := range runs {
		if opts.Labels[labelRole] == roleStage && strings.Contains(opts.Name, "cleanup") {
			cleanups++
		}
	}
	if cleanups == 0 {
		t.Error("no staged-artifact cleanup container ran after the failed restore")
	}

	database, err := fixture.databases.GetDatabase(context.Background(), fixture.database.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if database.Status != StatusError {
		t.Errorf("database status = %q, want %q", database.Status, StatusError)
	}
}

func TestFailedRestoreAlwaysMarksError(t *testing.T) {
	t.Run("stopped database", func(t *testing.T) {
		fixture := newBackupFixture(t)
		backup := fixture.queueBackup(t)
		fixture.containers.mu.Lock()
		fixture.containers.listed = nil
		fixture.containers.logFn = failingRestoreLogs
		fixture.containers.mu.Unlock()

		result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
			RestoreRequest{BackupID: backup.ID})
		if err != nil {
			t.Fatalf("RestoreBackup: %v", err)
		}
		waitFor(t, "the restore to fail", func() bool {
			restore, ok := fixture.backups.getRestore(result.RestoreID)
			return ok && restore.Status != RestoreRunning
		})
		assertDatabaseError(t, fixture)
	})

	t.Run("no container", func(t *testing.T) {
		fixture := newBackupFixture(t)
		backup := fixture.queueBackup(t)
		if _, err := fixture.databases.UpdateDatabaseContainer(context.Background(), fixture.database.ID, ""); err != nil {
			t.Fatalf("clear container id: %v", err)
		}
		fixture.containers.mu.Lock()
		fixture.containers.logFn = failingRestoreLogs
		fixture.containers.mu.Unlock()

		result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
			RestoreRequest{BackupID: backup.ID})
		if err != nil {
			t.Fatalf("RestoreBackup: %v", err)
		}
		waitFor(t, "the restore to fail", func() bool {
			restore, ok := fixture.backups.getRestore(result.RestoreID)
			return ok && restore.Status != RestoreRunning
		})
		assertDatabaseError(t, fixture)
	})
}

func assertDatabaseError(t *testing.T, fixture *backupFixture) {
	t.Helper()
	database, err := fixture.databases.GetDatabase(context.Background(), fixture.database.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if database.Status != StatusError {
		t.Errorf("database status = %q, want %q", database.Status, StatusError)
	}
}

func TestReconcileRemovesOrphanJobBeforeResuming(t *testing.T) {
	fixture := newBackupFixture(t)
	if _, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
		WasRunning: true,
	}); err != nil {
		t.Fatalf("seed running backup: %v", err)
	}

	fixture.containers.mu.Lock()
	fixture.containers.listed = append(fixture.containers.listed, containers.Container{
		ID:    "orphan-job",
		State: "running",
		Labels: map[string]string{
			labelManaged:    "true",
			labelDatabaseID: fixture.database.ID.String(),
			labelRole:       roleRestore,
		},
	})
	fixture.containers.mu.Unlock()

	fixture.manager.reconcileStaleBackups()

	fixture.containers.mu.Lock()
	removes := append([]string(nil), fixture.containers.removes...)
	calls := append([]string(nil), fixture.containers.calls...)
	fixture.containers.mu.Unlock()

	if !containsString(removes, "orphan-job") {
		t.Errorf("orphan job was not removed: removes = %v", removes)
	}
	if containsString(removes, fixture.database.ContainerID) {
		t.Errorf("the database container was removed: removes = %v", removes)
	}
	removeAt, startAt := indexOf(calls, "remove"), indexOf(calls, "start")
	if removeAt < 0 || startAt < 0 || removeAt > startAt {
		t.Errorf("orphan cleanup must precede the resume: calls = %v", calls)
	}
}

func TestReconcileSkipsDatabaseWithLiveLease(t *testing.T) {
	fixture := newBackupFixture(t)
	live, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
	})
	if err != nil {
		t.Fatalf("seed running backup: %v", err)
	}
	if !fixture.manager.claim(fixture.database.ID) {
		t.Fatal("could not claim the live lease")
	}
	defer fixture.manager.release(fixture.database.ID)

	fixture.manager.reconcileStaleBackups()

	fixture.containers.mu.Lock()
	starts, removes := fixture.containers.starts, append([]string(nil), fixture.containers.removes...)
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want 0: a database with a live lease was resumed", starts)
	}
	if len(removes) != 0 {
		t.Errorf("removes = %v, want none: a live job's resources were cleaned", removes)
	}
	if got, _ := fixture.backups.getBackup(live.ID); got.Status != BackupRunning {
		t.Errorf("live backup status = %q, want %q: the sweep clobbered a live job", got.Status, BackupRunning)
	}
}

func TestReconcileSweepsInterruptedRestore(t *testing.T) {
	fixture := newBackupFixture(t)
	interrupted := fixture.backups.seedRestore(Restore{
		DatabaseID: fixture.database.ID,
		BackupID:   uuid.New(),
		Status:     RestoreRunning,
	})

	fixture.manager.reconcileStaleBackups()

	restore, ok := fixture.backups.getRestore(interrupted.ID)
	if !ok {
		t.Fatalf("interrupted restore %s disappeared", interrupted.ID)
	}
	if restore.Status != RestoreFailed {
		t.Errorf("restore status = %q, want %q", restore.Status, RestoreFailed)
	}
	if restore.Error == "" {
		t.Error("swept restore carries no error")
	}
	if restore.FinishedAt.IsZero() {
		t.Error("swept restore has no finished_at")
	}

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want 0: an interrupted restore was resumed", starts)
	}
	cleanups := 0
	for _, opts := range runs {
		if opts.Labels[labelRole] == roleStage && strings.Contains(opts.Name, "cleanup") {
			cleanups++
		}
	}
	if cleanups == 0 {
		t.Error("the restore sweep did not clean the staged artifact")
	}
	database, err := fixture.databases.GetDatabase(context.Background(), fixture.database.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if database.Status != StatusError {
		t.Errorf("database status = %q, want %q", database.Status, StatusError)
	}
}

func TestReconcileDoesNotStartUserStoppedDatabase(t *testing.T) {
	fixture := newBackupFixture(t)
	if _, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
		WasRunning: false,
	}); err != nil {
		t.Fatalf("seed running backup: %v", err)
	}

	fixture.manager.reconcileStaleBackups()

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want 0: a user-stopped database was restarted", starts)
	}
}

func TestReconcileCompoundStaleRowsNeverResume(t *testing.T) {
	fixture := newBackupFixture(t)
	if _, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
		WasRunning: true,
	}); err != nil {
		t.Fatalf("seed running backup: %v", err)
	}
	fixture.backups.seedRestore(Restore{
		DatabaseID: fixture.database.ID,
		BackupID:   uuid.New(),
		Status:     RestoreRunning,
	})

	fixture.manager.reconcileStaleBackups()

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want 0: a compound stale pair resumed onto partial data", starts)
	}
	database, err := fixture.databases.GetDatabase(context.Background(), fixture.database.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if database.Status != StatusError {
		t.Errorf("database status = %q, want %q", database.Status, StatusError)
	}
}

func TestReconcileSkipsRestoreWithLiveLease(t *testing.T) {
	fixture := newBackupFixture(t)
	live := fixture.backups.seedRestore(Restore{
		DatabaseID: fixture.database.ID,
		BackupID:   uuid.New(),
		Status:     RestoreRunning,
	})
	if !fixture.manager.claimRestore(fixture.database.ID) {
		t.Fatal("could not claim the live restore lease")
	}
	defer fixture.manager.release(fixture.database.ID)

	fixture.manager.reconcileStaleBackups()

	if got, _ := fixture.backups.getRestore(live.ID); got.Status != RestoreRunning {
		t.Errorf("live restore status = %q, want %q: the sweep clobbered a live job", got.Status, RestoreRunning)
	}
	fixture.containers.mu.Lock()
	starts, removes := fixture.containers.starts, append([]string(nil), fixture.containers.removes...)
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want 0", starts)
	}
	if len(removes) != 0 {
		t.Errorf("removes = %v, want none", removes)
	}
}

func TestLifecycleRespectsJobLease(t *testing.T) {
	fixture := newBackupFixture(t)
	svc := NewService(Config{
		Repository: fixture.databases,
		Containers: fixture.containers,
		Secret:     testSecret,
		Logger:     discardLogger(),
		Leases:     fixture.manager.leases,
	})
	bg := context.Background()

	if !fixture.manager.claim(fixture.database.ID) {
		t.Fatal("could not claim the backup lease")
	}
	if _, err := svc.Start(bg, fixture.userID, fixture.database.ID); !errors.Is(err, ErrDatabaseBusy) {
		t.Errorf("Start during a backup = %v, want ErrDatabaseBusy", err)
	}
	if _, err := svc.Restart(bg, fixture.userID, fixture.database.ID); !errors.Is(err, ErrDatabaseBusy) {
		t.Errorf("Restart during a backup = %v, want ErrDatabaseBusy", err)
	}
	fixture.manager.release(fixture.database.ID)

	if !fixture.manager.leases.Claim(fixture.database.ID, JobLeaseLifecycle) {
		t.Fatal("could not claim the lifecycle lease")
	}
	if _, err := fixture.manager.CreateBackup(bg, fixture.userID, fixture.database.ID, CreateBackupRequest{}); !errors.Is(err, ErrBackupInFlight) {
		t.Errorf("CreateBackup during a lifecycle action = %v, want ErrBackupInFlight", err)
	}
	if _, err := svc.Start(bg, fixture.userID, fixture.database.ID); !errors.Is(err, ErrDatabaseBusy) ||
		!strings.Contains(err.Error(), "another operation") {
		t.Errorf("double lifecycle Start = %v, want ErrDatabaseBusy with a neutral reason", err)
	}
	fixture.manager.leases.Release(fixture.database.ID)

	if _, err := svc.Start(bg, fixture.userID, fixture.database.ID); err != nil {
		t.Fatalf("Start with a free lease: %v", err)
	}
}

type gatedObjectStore struct {
	release chan struct{}
}

var _ ObjectStore = (*gatedObjectStore)(nil)

func (g *gatedObjectStore) Kind() string { return "s3" }

func (g *gatedObjectStore) Put(context.Context, string, io.Reader, int64) (string, error) {
	return locationS3Prefix + "bucket/gated", nil
}

func (g *gatedObjectStore) Get(context.Context, string) (io.ReadCloser, error) {
	<-g.release
	return io.NopCloser(strings.NewReader("payload")), nil
}

func (g *gatedObjectStore) Delete(context.Context, string) error { return nil }

func TestRestoreHoldsJobLease(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)

	gate := &gatedObjectStore{release: make(chan struct{})}
	fixture.manager.objects = gate

	svc := NewService(Config{
		Repository: fixture.databases,
		Containers: fixture.containers,
		Secret:     testSecret,
		Logger:     discardLogger(),
		Leases:     fixture.manager.leases,
	})
	bg := context.Background()

	result, err := fixture.manager.RestoreBackup(bg, fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: backup.ID})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if _, err := svc.Start(bg, fixture.userID, fixture.database.ID); !errors.Is(err, ErrDatabaseBusy) {
		t.Errorf("Start during a restore = %v, want ErrDatabaseBusy", err)
	}
	if _, err := fixture.manager.CreateBackup(bg, fixture.userID, fixture.database.ID, CreateBackupRequest{}); !errors.Is(err, ErrBackupInFlight) {
		t.Errorf("CreateBackup during a restore = %v, want ErrBackupInFlight", err)
	}

	close(gate.release)
	waitFor(t, "the restore to finish", func() bool {
		restore, ok := fixture.backups.getRestore(result.RestoreID)
		return ok && restore.Status != RestoreRunning
	})

	completed := fixture.backups.seedBackup(Backup{DatabaseID: fixture.database.ID, Status: BackupCompleted})
	if !fixture.manager.claim(fixture.database.ID) {
		t.Fatal("could not claim the backup lease")
	}
	defer fixture.manager.release(fixture.database.ID)
	if _, err := fixture.manager.RestoreBackup(bg, fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: completed.ID}); !errors.Is(err, ErrBackupInFlight) {
		t.Errorf("RestoreBackup during a backup = %v, want ErrBackupInFlight", err)
	}
}

func TestBackupRecordsWasRunning(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		fixture := newBackupFixture(t)
		backup := fixture.queueBackup(t)
		got, _ := fixture.backups.getBackup(backup.ID)
		if !got.WasRunning {
			t.Error("WasRunning = false, want true for a running database")
		}
	})

	t.Run("stopped", func(t *testing.T) {
		fixture := newBackupFixture(t)
		fixture.containers.mu.Lock()
		fixture.containers.listed = nil
		fixture.containers.mu.Unlock()
		backup := fixture.queueBackup(t)
		got, _ := fixture.backups.getBackup(backup.ID)
		if got.WasRunning {
			t.Error("WasRunning = true, want false for a stopped database")
		}
	})
}

func TestBackupAbortsBeforeStoppingWhenStateCannotBeRecorded(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.backups.setWasRunningErr = errors.New("metadata store unavailable")

	queued, err := fixture.manager.CreateBackup(context.Background(), fixture.userID, fixture.database.ID, CreateBackupRequest{})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	finished := fixture.waitBackup(t, queued.ID)
	if finished.Status != BackupFailed {
		t.Errorf("status = %q, want %q", finished.Status, BackupFailed)
	}
	if !strings.Contains(finished.Error, "record the pre-backup database state") {
		t.Errorf("error = %q, want the record-write cause", finished.Error)
	}
	fixture.containers.mu.Lock()
	stops := fixture.containers.stops
	fixture.containers.mu.Unlock()
	if stops != 0 {
		t.Errorf("stops = %d, want 0: the database was stopped before its state was recorded", stops)
	}
}

func TestReconcileDoesNotResumeWhenRestoreListUnavailable(t *testing.T) {
	fixture := newBackupFixture(t)
	if _, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
		WasRunning: true,
	}); err != nil {
		t.Fatalf("seed running backup: %v", err)
	}
	fixture.backups.listRestoresErr = errors.New("metadata store unavailable")

	fixture.manager.reconcileStaleBackups()

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	fixture.containers.mu.Unlock()
	if starts != 0 {
		t.Errorf("starts = %d, want 0: a backup was resumed while the restore list was unreadable", starts)
	}
}

type failingReader struct{ done bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("artifact stream failed")
	}
	r.done = true
	return copy(p, "partial"), nil
}

type readerObjectStore struct{ reader io.ReadCloser }

var _ ObjectStore = (*readerObjectStore)(nil)

func (s *readerObjectStore) Kind() string { return "s3" }

func (s *readerObjectStore) Put(context.Context, string, io.Reader, int64) (string, error) {
	return locationS3Prefix + "bucket/reader", nil
}

func (s *readerObjectStore) Get(context.Context, string) (io.ReadCloser, error) {
	return s.reader, nil
}

func (s *readerObjectStore) Delete(context.Context, string) error { return nil }

func TestStagingFailureCleansStagedArtifact(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.backups.seedBackup(Backup{
		DatabaseID: fixture.database.ID,
		Status:     BackupCompleted,
		Location:   locationS3Prefix + "bucket/reader",
	})
	fixture.manager.objects = &readerObjectStore{reader: io.NopCloser(&failingReader{})}

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: backup.ID})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	waitFor(t, "the restore to fail", func() bool {
		restore, ok := fixture.backups.getRestore(result.RestoreID)
		return ok && restore.Status != RestoreRunning
	})

	fixture.containers.mu.Lock()
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	fixture.containers.mu.Unlock()
	cleanups := 0
	for _, opts := range runs {
		if opts.Labels[labelRole] == roleStage && strings.Contains(opts.Name, "cleanup") {
			cleanups++
		}
	}
	if cleanups == 0 {
		t.Error("a mid-stream staging failure did not clean the staged artifact")
	}
	assertDatabaseError(t, fixture)
}

func containsString(list []string, want string) bool {
	return indexOf(list, want) >= 0
}

func indexOf(list []string, want string) int {
	for i, item := range list {
		if item == want {
			return i
		}
	}
	return -1
}
