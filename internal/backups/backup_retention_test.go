package backups

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func intPtr(value int) *int { return &value }

// makeCleanupDue rewinds the retry backoff so the test can sweep immediately.
func (r *fakeBackupRepository) makeCleanupDue(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	backup := r.backups[id]
	backup.CleanupNextAttemptAt = time.Now().UTC().Add(-time.Minute)
	r.backups[id] = backup
}

func writeArtifact(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("dump"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return locationFilePrefix + path
}

func TestRetentionKeepsTheNewestScheduledBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		KeepLast:    intPtr(3),
	})
	now := time.Now().UTC()

	// Oldest first, so the repository's newest-first order matches created_at.
	scheduled := make([]Backup, 0, 5)
	for i := 4; i >= 0; i-- {
		backup := fixture.backups.seedBackup(Backup{
			ResourceRef: databaseRef(fixture.databaseID),
			ScheduleID:  schedule.ID,
			Type:        BackupScheduled,
			Status:      BackupCompleted,
			Location:    writeArtifact(t, fixture.dir, "scheduled-"+uuid.NewString()),
			CreatedAt:   now.Add(-time.Duration(i) * time.Hour),
		})
		scheduled = append(scheduled, backup)
	}
	// scheduled[4] is newest; the three newest are indexes 2, 3 and 4.
	manual := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupCompleted,
		Location:    writeArtifact(t, fixture.dir, "manual"),
		CreatedAt:   now.Add(-100 * time.Hour),
	})
	failed := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		ScheduleID:  schedule.ID,
		Type:        BackupScheduled,
		Status:      BackupFailed,
		Location:    writeArtifact(t, fixture.dir, "failed"),
		CreatedAt:   now.Add(-90 * time.Hour),
	})
	running := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		ScheduleID:  schedule.ID,
		Type:        BackupScheduled,
		Status:      BackupRunning,
		Location:    writeArtifact(t, fixture.dir, "running"),
		CreatedAt:   now.Add(-80 * time.Hour),
	})

	fixture.manager.pruneSchedule(ctx, schedule.ID)

	for index, backup := range scheduled {
		_, ok := fixture.backups.getBackup(backup.ID)
		if index <= 1 && ok {
			t.Errorf("old scheduled run %d was not pruned", index)
		}
		if index >= 2 && !ok {
			t.Errorf("scheduled run %d should be kept", index)
		}
	}
	if _, ok := fixture.backups.getBackup(manual.ID); !ok {
		t.Error("a manual backup must never be pruned by retention")
	}
	if _, ok := fixture.backups.getBackup(failed.ID); !ok {
		t.Error("a failed run must not be counted towards keep_last")
	}
	if _, ok := fixture.backups.getBackup(running.ID); !ok {
		t.Error("a running run must never be pruned")
	}
}

func TestRetentionWithoutALimitKeepsEverything(t *testing.T) {
	fixture := newBackupFixture(t)
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
	})
	for i := 0; i < 5; i++ {
		fixture.backups.seedBackup(Backup{
			ResourceRef: databaseRef(fixture.databaseID),
			ScheduleID:  schedule.ID,
			Type:        BackupScheduled,
			Status:      BackupCompleted,
			CreatedAt:   time.Now().UTC().Add(-time.Duration(i) * time.Hour),
		})
	}
	fixture.manager.pruneSchedule(context.Background(), schedule.ID)
	if got := fixture.backups.countBackups(); got != 5 {
		t.Fatalf("backups = %d, want all 5 kept when no limit is set", got)
	}
}

func TestCleanupPendingSurvivesAProviderOutageAndRetries(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	objects := newFakeObjectStore()
	objects.deleteErr = errors.New("provider offline")
	fixture.manager.objects = objects
	backup := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupCompleted,
		Location:    locationS3Prefix + "bucket/key.gz",
	})

	err := fixture.manager.DeleteBackup(ctx, fixture.userID, backup.ID)
	if !errors.Is(err, ErrCleanupFailed) {
		t.Fatalf("DeleteBackup err = %v, want ErrCleanupFailed", err)
	}
	stored, ok := fixture.backups.getBackup(backup.ID)
	if !ok {
		t.Fatal("an unreachable artifact must not lose its row")
	}
	if !stored.CleanupPending || stored.CleanupError == "" {
		t.Fatalf("backup = %+v, want cleanup pending with the last error", stored)
	}
	if stored.Status == BackupFailed {
		t.Fatal("a cleanup failure must not mark the run failed")
	}

	objects.deleteErr = nil
	fixture.backups.makeCleanupDue(backup.ID)
	removed, err := fixture.manager.SweepCleanup(ctx)
	if err != nil {
		t.Fatalf("SweepCleanup: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := fixture.backups.getBackup(backup.ID); ok {
		t.Fatal("the row must be removed once the provider accepts the delete")
	}
	if len(objects.deletes) != 1 {
		t.Fatalf("deletes = %v, want one artifact deletion", objects.deletes)
	}
}

func TestSweepPrunesFailedRunsAfterTheRetentionWindow(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	old := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupScheduled,
		Status:      BackupFailed,
		CreatedAt:   time.Now().UTC().Add(-FailedBackupRetention - time.Hour),
	})
	recent := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupScheduled,
		Status:      BackupFailed,
		CreatedAt:   time.Now().UTC().Add(-time.Hour),
	})
	if _, err := fixture.manager.SweepCleanup(ctx); err != nil {
		t.Fatalf("SweepCleanup: %v", err)
	}
	if _, ok := fixture.backups.getBackup(old.ID); ok {
		t.Error("a failed run older than the window should be pruned")
	}
	if _, ok := fixture.backups.getBackup(recent.ID); !ok {
		t.Error("a recent failed run must be kept")
	}
}

func TestDeleteProviderRequiresConfirmationForPastBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	target := fixture.backups.seedTarget(BackupTarget{
		UserID: fixture.userID, Name: "offsite", Kind: TargetS3,
	})
	backup := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupCompleted,
		TargetID:    target.ID,
		Location:    locationS3Prefix + "bucket/key.gz",
	})
	err := fixture.manager.DeleteProvider(context.Background(), fixture.userID, target.ID, false)
	if !errors.Is(err, ErrProviderHasBackups) {
		t.Fatalf("DeleteProvider err = %v, want ErrProviderHasBackups", err)
	}
	if err := fixture.manager.DeleteProvider(context.Background(), fixture.userID, target.ID, true); err != nil {
		t.Fatalf("confirmed DeleteProvider: %v", err)
	}
	stored, ok := fixture.backups.getBackup(backup.ID)
	if !ok {
		t.Fatal("a run must stay listed after its provider is deleted")
	}
	if !stored.ProviderRemoved {
		t.Fatal("the run must be marked provider removed")
	}
}

func TestLocalDiskGuardFailsBeforeDumping(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupCompleted,
		Size:        1 << 60,
	})
	_, err := fixture.manager.CreateBackup(context.Background(), fixture.userID,
		CreateBackupRequest{Resource: databaseRef(fixture.databaseID)})
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("CreateBackup err = %v, want ErrInsufficientSpace", err)
	}
	fixture.source.mu.Lock()
	defer fixture.source.mu.Unlock()
	if len(fixture.source.captures) != 0 {
		t.Fatal("the driver must not run when the disk guard rejects the job")
	}
}

func TestScheduleKeepLastDefaultsToSevenAndValidates(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	created, err := fixture.manager.CreateSchedule(ctx, fixture.userID, ScheduleRequest{
		Resource: databaseRef(fixture.databaseID),
		Cron:     "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.KeepLast == nil || *created.KeepLast != DefaultKeepLastScheduledBackups {
		t.Fatalf("keep_last = %v, want the default %d", created.KeepLast, DefaultKeepLastScheduledBackups)
	}
	if _, err := fixture.manager.CreateSchedule(ctx, fixture.userID, ScheduleRequest{
		Resource: databaseRef(fixture.databaseID),
		Cron:     "0 3 * * *",
		KeepLast: intPtr(0),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("keep_last=0 err = %v, want ErrValidation", err)
	}
}
