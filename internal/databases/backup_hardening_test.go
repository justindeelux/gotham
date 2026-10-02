package databases

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/teams"
)

// TestBackupRejectsTargetOfAnotherTeamMember is the D2-7 regression: a team
// member can back up a database they share, but only to a target their
// database owner owns. Before the fix the member's own S3 target was accepted
// and the read path — which resolves the target by the database owner — then
// answered 404 on every restore.
func TestBackupRejectsTargetOfAnotherTeamMember(t *testing.T) {
	owner, member := uuid.New(), uuid.New()
	teamID := uuid.New()

	databaseRepo := newFakeRepository()
	serverID := databaseRepo.seedServer()
	database := databaseRepo.seed(Database{
		UserID:      owner,
		TeamID:      teamID,
		ServerID:    serverID,
		Name:        "shared-db",
		Engine:      EnginePostgres,
		Status:      StatusRunning,
		ContainerID: "db-container",
		StoragePath: VolumeName(uuid.New()),
	})
	backupRepo := newFakeBackupRepository()
	manager := NewBackupService(BackupConfig{
		Repository:         backupRepo,
		DatabaseRepository: databaseRepo,
		Containers:         &fakeContainers{},
		ObjectStore:        newFakeObjectStore(),
		Secret:             testSecret,
		Logger:             discardLogger(),
	})
	t.Cleanup(func() { _ = manager.Close() })

	ownerTarget := backupRepo.seedTarget(BackupTarget{
		UserID: owner, Name: "owner", Kind: TargetS3, Endpoint: "http://minio:9000", Bucket: "b",
	})
	memberTarget := backupRepo.seedTarget(BackupTarget{
		UserID: member, Name: "member", Kind: TargetS3, Endpoint: "http://minio:9000", Bucket: "b",
	})

	memberCtx := teams.WithScope(context.Background(), teams.Scope{
		UserID: member, TeamID: teamID, Role: teams.RoleAdmin,
	})
	if _, err := manager.CreateBackup(memberCtx, member, database.ID, CreateBackupRequest{TargetID: memberTarget.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-member target CreateBackup err = %v, want ErrNotFound", err)
	}
	if backupRepo.countBackups() != 0 {
		t.Fatal("a refused cross-member target must not record a run")
	}
	if _, err := manager.CreateSchedule(memberCtx, member, database.ID, ScheduleRequest{
		Cron: "0 2 * * *", TargetID: pointerTo(memberTarget.ID.String()),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-member target CreateSchedule err = %v, want ErrNotFound", err)
	}

	// The owner's own target is accepted and completes.
	ownerCtx := teams.WithScope(context.Background(), teams.Scope{
		UserID: owner, TeamID: teamID, Role: teams.RoleOwner,
	})
	queued, err := manager.CreateBackup(ownerCtx, owner, database.ID, CreateBackupRequest{TargetID: ownerTarget.ID})
	if err != nil {
		t.Fatalf("owner target CreateBackup: %v", err)
	}
	waitForBackupDone(t, backupRepo, queued.ID)
}

// pointerTo is a small helper for the tri-state target field.
func pointerTo(value string) *string { return &value }

// waitForBackupDone blocks until a run reaches a terminal state.
func waitForBackupDone(t *testing.T, repo *fakeBackupRepository, backupID uuid.UUID) Backup {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if backup, ok := repo.getBackup(backupID); ok && backup.Status != BackupRunning {
			return backup
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("backup %s never left the running state", backupID)
	return Backup{}
}

// TestNextCronTimeSurvivesSpringForward is the D2-8 regression: crossing the
// America/New_York spring-forward gap used to leave the cursor stuck, looping
// forever. The call must return promptly and strictly after the input.
func TestNextCronTimeSurvivesSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// 2025-03-09 02:00 local does not exist; the schedule asks for it.
	after := time.Date(2025, 3, 9, 1, 0, 0, 0, loc)

	type answer struct {
		next time.Time
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		next, err := nextCronTime("0 2 * * *", after, loc)
		done <- answer{next: next, err: err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("nextCronTime: %v", got.err)
		}
		if !got.next.After(after) {
			t.Errorf("next = %v, want strictly after %v", got.next, after)
		}
		if got.next.Hour() != 2 || got.next.Minute() != 0 {
			t.Errorf("next = %v, want the next 02:00 local", got.next)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nextCronTime did not return across the spring-forward gap")
	}
}

// TestLocalStoreFlushesBeforeReturning is the D2-9 half that pins the flush:
// Put must fsync both the file and its directory before reporting success, so
// the completion write that follows cannot outlive the artifact.
func TestLocalStoreFlushesBeforeReturning(t *testing.T) {
	store, err := newLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	originalFile, originalDir := fsyncFile, fsyncDir
	t.Cleanup(func() { fsyncFile, fsyncDir = originalFile, originalDir })

	var fileFlushed, dirFlushed bool
	fsyncFile = func(file *os.File) error { fileFlushed = true; return originalFile(file) }
	fsyncDir = func(dir string) error { dirFlushed = true; return originalDir(dir) }

	payload := []byte("payload")
	if _, err := store.Put(context.Background(), "k", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !fileFlushed || !dirFlushed {
		t.Errorf("flushed file=%v dir=%v, want both", fileFlushed, dirFlushed)
	}
}

// TestLocalStoreRemovesArtifactWhenFlushFails proves a failed flush is not
// reported as a stored artifact.
func TestLocalStoreRemovesArtifactWhenFlushFails(t *testing.T) {
	root := t.TempDir()
	store, err := newLocalStore(root)
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	originalFile := fsyncFile
	t.Cleanup(func() { fsyncFile = originalFile })
	fsyncFile = func(*os.File) error { return errors.New("flush failed") }

	if _, err := store.Put(context.Background(), "k", bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("Put reported success although the flush failed")
	}
	if _, statErr := os.Stat(root + "/k"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a failed flush left an artifact behind: %v", statErr)
	}
}

// TestBackupFlushesArtifactBeforeRecordingCompletion is the D2-9 ordering
// regression at the manager level: the run must not be marked completed before
// the local store flushed the artifact.
func TestBackupFlushesArtifactBeforeRecordingCompletion(t *testing.T) {
	fixture := newBackupFixture(t)
	originalFile := fsyncFile
	t.Cleanup(func() { fsyncFile = originalFile })

	flushed := false
	fsyncFile = func(file *os.File) error { flushed = true; return originalFile(file) }
	violation := false
	fixture.backups.finishHook = func(backup Backup) {
		if backup.Status == BackupCompleted && !flushed {
			violation = true
		}
	}

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
	if violation {
		t.Error("the run was marked completed before the artifact was flushed")
	}
}

// TestUpdateTargetLocksDestinationWithCompletedBackups is the D2-10
// regression: moving where a target's artifacts live would strand every
// completed backup that reads from the old destination.
func TestUpdateTargetLocksDestinationWithCompletedBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target, err := fixture.manager.CreateTarget(ctx, fixture.userID, TargetRequest{
		Name: "s3", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "old",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	fixture.backups.seedBackup(Backup{
		DatabaseID: fixture.database.ID, Status: BackupCompleted,
		TargetID: target.ID, Location: locationS3Prefix + "old/key.gz",
	})

	if _, err := fixture.manager.UpdateTarget(ctx, fixture.userID, target.ID, TargetRequest{Bucket: "new"}); !errors.Is(err, ErrTargetStranded) {
		t.Fatalf("bucket move err = %v, want ErrTargetStranded", err)
	}
	stored, err := fixture.backups.GetBackupTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if stored.Bucket != "old" {
		t.Errorf("bucket = %q, want the locked old value", stored.Bucket)
	}

	// Renaming and changing the (read-irrelevant) prefix stay allowed.
	if _, err := fixture.manager.UpdateTarget(ctx, fixture.userID, target.ID, TargetRequest{
		Name: "renamed", Prefix: "new-prefix/",
	}); err != nil {
		t.Fatalf("name/prefix update: %v", err)
	}

	// A running run locks the destination too: it captured the old
	// configuration and will record the old location when it finishes.
	runningTarget, err := fixture.manager.CreateTarget(ctx, fixture.userID, TargetRequest{
		Name: "s3-running", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "old",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("CreateTarget(running): %v", err)
	}
	fixture.backups.seedBackup(Backup{
		DatabaseID: fixture.database.ID, Status: BackupRunning, TargetID: runningTarget.ID,
	})
	if _, err := fixture.manager.UpdateTarget(ctx, fixture.userID, runningTarget.ID, TargetRequest{Bucket: "moved"}); !errors.Is(err, ErrTargetStranded) {
		t.Fatalf("running-backup bucket move err = %v, want ErrTargetStranded", err)
	}
}

// TestUpdateTargetIsAtomicWhenSealingFails is the D2-11 regression: a failed
// credential write must not leave the new configuration paired with the old
// credentials.
func TestUpdateTargetIsAtomicWhenSealingFails(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target := fixture.backups.seedTarget(BackupTarget{
		UserID: fixture.userID, Name: "s3", Kind: TargetS3,
		Endpoint: "https://old.example.com", Bucket: "b",
	})
	fixture.backups.secretErr = errors.New("credential write failed")

	if _, err := fixture.manager.UpdateTarget(ctx, fixture.userID, target.ID, TargetRequest{
		Endpoint: "https://new.example.com", AccessKey: "ak", SecretKey: "sk",
	}); err == nil {
		t.Fatal("expected the credential failure to surface")
	}
	stored, err := fixture.backups.GetBackupTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if stored.Endpoint != "https://old.example.com" {
		t.Errorf("endpoint = %q, want the transaction to have rolled back", stored.Endpoint)
	}
}

// TestSweepJobContainers is the D2-13 regression: the startup sweep removes
// leftover job containers but never the database container nor a container
// whose run/database is still leased.
func TestSweepJobContainers(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.backups.serverIDs = []uuid.UUID{fixture.serverID}

	liveRun := fixture.backups.seedBackup(Backup{
		DatabaseID: fixture.database.ID, Status: BackupRunning,
	})
	leasedDB := uuid.New()
	if !fixture.manager.claim(leasedDB) {
		t.Fatal("could not lease the database")
	}
	defer fixture.manager.release(leasedDB)

	fixture.containers.listed = []containers.Container{
		{ID: "db-container", Labels: map[string]string{
			labelManaged: "true", labelDatabaseID: fixture.database.ID.String(),
		}},
		{ID: "leftover-backup", Labels: map[string]string{
			labelManaged: "true", labelRole: roleBackup,
			labelDatabaseID: uuid.New().String(), labelBackupID: uuid.New().String(),
		}},
		{ID: "live-restore", Labels: map[string]string{
			labelManaged: "true", labelRole: roleRestore,
			labelDatabaseID: uuid.New().String(), labelBackupID: liveRun.ID.String(),
		}},
		{ID: "leased-stage", Labels: map[string]string{
			labelManaged: "true", labelRole: roleStage,
			labelDatabaseID: leasedDB.String(), labelBackupID: uuid.New().String(),
		}},
		{ID: "unmanaged", Labels: map[string]string{
			labelRole: roleBackup, labelDatabaseID: uuid.New().String(),
		}},
	}

	fixture.manager.sweepJobContainers()

	fixture.containers.mu.Lock()
	removes := append([]string(nil), fixture.containers.removes...)
	fixture.containers.mu.Unlock()
	if len(removes) != 1 || removes[0] != "leftover-backup" {
		t.Errorf("removed %v, want exactly [leftover-backup]", removes)
	}
}

// TestClampBackupListLimit pins the listing bound.
func TestClampBackupListLimit(t *testing.T) {
	cases := map[int]int{
		0:                      defaultBackupListLimit,
		-1:                     defaultBackupListLimit,
		1:                      1,
		defaultBackupListLimit: defaultBackupListLimit,
		maxBackupListLimit:     maxBackupListLimit,
		maxBackupListLimit + 1: maxBackupListLimit,
	}
	for input, want := range cases {
		if got := clampBackupListLimit(input); got != want {
			t.Errorf("clampBackupListLimit(%d) = %d, want %d", input, got, want)
		}
	}
}

// TestListBackupsRespectsLimit proves the service caps the page it asks the
// repository for, so a database with a year of runs cannot load them all.
func TestListBackupsRespectsLimit(t *testing.T) {
	fixture := newBackupFixture(t)
	for i := 0; i < 5; i++ {
		fixture.backups.seedBackup(Backup{DatabaseID: fixture.database.ID, Status: BackupCompleted})
	}
	backups, err := fixture.manager.ListBackups(context.Background(), fixture.userID, fixture.database.ID, 2)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 2 {
		t.Errorf("listed %d backups, want 2", len(backups))
	}
}
