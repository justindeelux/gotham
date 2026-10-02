package databases

import (
	"context"
	"errors"
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
	fixture.containers.mu.Unlock()
	if starts != startsBefore {
		t.Errorf("starts = %d, want %d: the database was restarted into a partial restore", starts, startsBefore)
	}
	if stops <= 1 {
		t.Errorf("stops = %d, want the restore to have paused the database", stops)
	}

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
	if _, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
	}); err != nil {
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
}

// TestReconcileSweepsInterruptedRestore is the D2-4 regression: before this
// fix an interrupted restore left no record at all, so a control-plane
// restart could neither report nor recover it. The persisted restore row is
// swept to failed and the database resumed.
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
	fixture.containers.mu.Unlock()
	if starts == 0 {
		t.Error("database container was not resumed after the restore sweep")
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

	// A lifecycle action owns the lease: a job cannot claim underneath it.
	if !fixture.manager.leases.Claim(fixture.database.ID, JobLeaseLifecycle) {
		t.Fatal("could not claim the lifecycle lease")
	}
	if _, err := fixture.manager.CreateBackup(bg, fixture.userID, fixture.database.ID, CreateBackupRequest{}); !errors.Is(err, ErrBackupInFlight) {
		t.Errorf("CreateBackup during a lifecycle action = %v, want ErrBackupInFlight", err)
	}
	fixture.manager.leases.Release(fixture.database.ID)

	// With the lease free, the lifecycle action succeeds again.
	if _, err := svc.Start(bg, fixture.userID, fixture.database.ID); err != nil {
		t.Fatalf("Start with a free lease: %v", err)
	}
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
