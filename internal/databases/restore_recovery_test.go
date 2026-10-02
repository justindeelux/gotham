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

// failingRestoreLogs scripts the staging containers successfully and the
// restore job as a mid-way failure, modelling an engine that exits nonzero
// after it has already written part of the data directory.
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

// TestFailedRestoreLeavesDatabaseStopped is the D2-2 regression: a restore
// that fails partway must not restart the database into a half-restored
// volume. Before the fix the resume was deferred unconditionally, so a
// nontransactional MySQL/Mongo/tar restore reported success and served
// partial data.
func TestFailedRestoreLeavesDatabaseStopped(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)

	// The dump job left its temporary container as the last observed one;
	// republish the database container so the restore pause sees it running.
	fixture.containers.setRunning(fixture.database.ContainerID)

	// The dump resumed the database once; the failing restore must not add a
	// second resume.
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

	// The failed restore must run the staged-artifact cleanup job, or a retry
	// starts on top of a partial file.
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

// TestFailedRestoreAlwaysMarksError is the U2 regression: a restore failure on
// an already-stopped database (or one with no container) must still leave a
// terminal error, not a stale running/stopped row.
func TestFailedRestoreAlwaysMarksError(t *testing.T) {
	t.Run("stopped database", func(t *testing.T) {
		fixture := newBackupFixture(t)
		backup := fixture.queueBackup(t)
		// No container is listed, so pauseDatabase reports it was not running.
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

// assertDatabaseError asserts the database row reached the terminal error
// state after a failed restore.
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

// TestReconcileRemovesOrphanJobBeforeResuming is the D2-3 regression: the
// boot-time sweep used to resume a database while a crashed restore's job
// container still owned its volume. The orphan container must be removed
// before the container is started.
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

	// An orphan restore container still mounted on the database volume.
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

// TestReconcileSkipsDatabaseWithLiveLease pins the lease check half of D2-3:
// the sweep must not touch a database whose volume a live job still owns in
// this process, even if a stale running row exists.
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
	// U4: the live job's row must not be clobbered to failed by the sweep.
	if got, _ := fixture.backups.getBackup(live.ID); got.Status != BackupRunning {
		t.Errorf("live backup status = %q, want %q: the sweep clobbered a live job", got.Status, BackupRunning)
	}
}

// TestReconcileSweepsInterruptedRestore is the D2-4 regression plus the U1
// rule: an interrupted restore is recorded, swept to failed, and the database
// is left stopped and marked error. It is never resumed, because the restore
// may have written partial data (the D2-2 invariant).
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
	// The sweep must remove the staged .part the crashed restore left behind.
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

// TestReconcileDoesNotStartUserStoppedDatabase is the U1 regression for a
// crashed backup: the sweep must not start a database the user had stopped.
// Only a backup that actually paused a running database may be resumed.
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

// TestReconcileCompoundStaleRowsNeverResume is the U1 (round 2) regression:
// when a database has both a stale backup (was_running=true) and a stale
// restore, the backup branch must not resume the database onto the restore's
// partial data. It stays stopped and error.
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

// TestReconcileSkipsRestoreWithLiveLease pins the restore-loop lease check:
// a restore row whose job is still live must not be clobbered or cleaned.
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

// TestLifecycleRespectsJobLease is the D2-5 regression, both directions: a
// Start/Restart during a held backup/restore lease is refused with
// ErrDatabaseBusy, and a backup/restore cannot claim a database while a
// lifecycle action holds the lease.
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

	// A backup owns the volume: lifecycle actions are refused.
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

	// A lifecycle action owns the lease: a job cannot claim underneath it,
	// and a second lifecycle action reports a neutral reason (U5), not a
	// misleading "backup or restore is running".
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

	// With the lease free, the lifecycle action succeeds again.
	if _, err := svc.Start(bg, fixture.userID, fixture.database.ID); err != nil {
		t.Fatalf("Start with a free lease: %v", err)
	}
}

// gatedObjectStore blocks Get until release is closed, so a test can observe
// the restore job's lease while it is in flight.
type gatedObjectStore struct {
	release chan struct{}
}

// Compile-time guarantee.
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

// TestRestoreHoldsJobLease is the U3 regression: RestoreBackup must claim the
// restore lease (replacing claimRestore with `return true` must fail here), so
// Start is refused while the job runs, and a backup lease blocks a restore.
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
	// The job is blocked inside store.Get; the restore lease is held.
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

	// The lease is free again: a completed backup can be restored once more.
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

// TestBackupRecordsWasRunning is the U3 (round 2) regression: the dump must
// persist the observed pre-job state. Replacing SetBackupWasRunning with a
// no-op must fail the running case.
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

// failingReader yields a few bytes and then an error, modelling an artifact
// download that dies mid-stream during staging.
type failingReader struct{ done bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("artifact stream failed")
	}
	r.done = true
	return copy(p, "partial"), nil
}

// readerObjectStore serves a fixed reader from Get.
type readerObjectStore struct{ reader io.ReadCloser }

// Compile-time guarantee.
var _ ObjectStore = (*readerObjectStore)(nil)

func (s *readerObjectStore) Kind() string { return "s3" }

func (s *readerObjectStore) Put(context.Context, string, io.Reader, int64) (string, error) {
	return locationS3Prefix + "bucket/reader", nil
}

func (s *readerObjectStore) Get(context.Context, string) (io.ReadCloser, error) {
	return s.reader, nil
}

func (s *readerObjectStore) Delete(context.Context, string) error { return nil }

// TestStagingFailureCleansStagedArtifact is the U4 (round 2) regression for
// the staging-failure branch: a download that errors mid-stream must still
// trigger the staged-artifact cleanup container.
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

// containsString reports whether list holds want.
func containsString(list []string, want string) bool {
	return indexOf(list, want) >= 0
}

// indexOf returns the first position of want, or -1.
func indexOf(list []string, want string) int {
	for i, item := range list {
		if item == want {
			return i
		}
	}
	return -1
}
