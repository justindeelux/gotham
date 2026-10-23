package databases

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/teams"
)

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

	ownerCtx := teams.WithScope(context.Background(), teams.Scope{
		UserID: owner, TeamID: teamID, Role: teams.RoleOwner,
	})
	queued, err := manager.CreateBackup(ownerCtx, owner, database.ID, CreateBackupRequest{TargetID: ownerTarget.ID})
	if err != nil {
		t.Fatalf("owner target CreateBackup: %v", err)
	}
	waitForBackupDone(t, backupRepo, queued.ID)
}

func pointerTo(value string) *string { return &value }

func TestUpdateScheduleTargetOwnership(t *testing.T) {
	owner, member := uuid.New(), uuid.New()
	teamID := uuid.New()

	databaseRepo := newFakeRepository()
	database := databaseRepo.seed(Database{
		UserID: owner, TeamID: teamID, ServerID: databaseRepo.seedServer(),
		Name: "shared-db", Engine: EnginePostgres, Status: StatusRunning,
		ContainerID: "db-container", StoragePath: VolumeName(uuid.New()),
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

	memberTarget := backupRepo.seedTarget(BackupTarget{
		UserID: member, Name: "member-s3", Kind: TargetS3, Endpoint: "http://minio:9000", Bucket: "b",
	})
	schedule := backupRepo.seedSchedule(BackupSchedule{
		DatabaseID: database.ID, Cron: "0 2 * * *", TargetID: memberTarget.ID,
		Enabled: true, NextRunAt: time.Now().UTC().Add(time.Hour),
	})
	ctx := teams.WithScope(context.Background(), teams.Scope{
		UserID: member, TeamID: teamID, Role: teams.RoleAdmin,
	})

	disabled := false
	updated, err := manager.UpdateSchedule(ctx, member, database.ID, schedule.ID, ScheduleRequest{
		Cron: "0 3 * * *", Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("cron-only UpdateSchedule: %v", err)
	}
	if updated.Enabled || updated.TargetID != memberTarget.ID {
		t.Errorf("updated = %+v, want disabled with the stored target kept", updated)
	}

	if _, err := manager.UpdateSchedule(ctx, member, database.ID, schedule.ID, ScheduleRequest{
		Cron: "0 4 * * *", TargetID: pointerTo(memberTarget.ID.String()),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner target UpdateSchedule err = %v, want ErrNotFound", err)
	}
}

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

func TestBackupValidatesTargetOwnershipUnderLock(t *testing.T) {
	owner, member := uuid.New(), uuid.New()
	teamID := uuid.New()

	databaseRepo := newFakeRepository()
	database := databaseRepo.seed(Database{
		UserID: owner, TeamID: teamID, ServerID: databaseRepo.seedServer(),
		Name: "shared-db", Engine: EnginePostgres, Status: StatusRunning,
		ContainerID: "db-container", StoragePath: VolumeName(uuid.New()),
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

	target := backupRepo.seedTarget(BackupTarget{UserID: member, Name: "flip", Kind: TargetLocal})
	backupRepo.beforeCreateBackupWithTarget = func() {
		flipped := backupRepo.targets[target.ID]
		flipped.Kind = TargetS3
		flipped.Endpoint = "http://minio:9000"
		flipped.Bucket = "b"
		backupRepo.targets[target.ID] = flipped
	}

	ctx := teams.WithScope(context.Background(), teams.Scope{
		UserID: member, TeamID: teamID, Role: teams.RoleAdmin,
	})
	if _, err := manager.CreateBackup(ctx, member, database.ID, CreateBackupRequest{TargetID: target.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("flipped cross-owner target err = %v, want ErrNotFound", err)
	}
	if backupRepo.countBackups() != 0 {
		t.Fatal("a refused target must leave no running row behind")
	}
}

func TestNextCronTimeSurvivesSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
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
	var got answer
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("nextCronTime did not return across the spring-forward gap")
	}
	if got.err != nil {
		t.Fatalf("nextCronTime: %v", got.err)
	}
	want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc)
	if !got.next.Equal(want) {
		t.Errorf("next = %v, want %v (first instant after the gap)", got.next, want)
	}
	if !got.next.After(after) {
		t.Errorf("next = %v, want strictly after %v", got.next, after)
	}
}

func TestNextCronTimeFallBackDoesNotRepeatHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 11, 1, 12, 0, 0, 0, loc)

	first, err := nextCronTime("30 1 * * *", after, loc)
	if err != nil {
		t.Fatalf("first nextCronTime: %v", err)
	}
	wantFirst := time.Date(2025, 11, 2, 1, 30, 0, 0, loc)
	if !first.Equal(wantFirst) {
		t.Fatalf("first = %v, want %v (EDT occurrence)", first, wantFirst)
	}
	second, err := nextCronTime("30 1 * * *", first, loc)
	if err != nil {
		t.Fatalf("second nextCronTime: %v", err)
	}
	wantSecond := time.Date(2025, 11, 3, 1, 30, 0, 0, loc)
	if !second.Equal(wantSecond) {
		t.Errorf("second = %v, want %v (no repeat of the 01:30 wall slot)", second, wantSecond)
	}
}

func TestNextCronTimeSpringForwardDoesNotMissDay(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 8, 12, 0, 0, 0, loc)

	first, err := nextCronTime("30 2 * * *", after, loc)
	if err != nil {
		t.Fatalf("nextCronTime: %v", err)
	}
	want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc)
	if !first.Equal(want) {
		t.Fatalf("next = %v, want %v (gap day not missed)", first, want)
	}
	second, err := nextCronTime("30 2 * * *", first, loc)
	if err != nil {
		t.Fatalf("second nextCronTime: %v", err)
	}
	wantSecond := time.Date(2025, 3, 10, 2, 30, 0, 0, loc)
	if !second.Equal(wantSecond) {
		t.Errorf("second = %v, want %v", second, wantSecond)
	}
}

func TestNextCronTimeStepsToNextTopOfHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 9, 1, 30, 0, 0, loc)
	got, err := nextCronTime("0 3 * * *", after, loc)
	if err != nil {
		t.Fatalf("nextCronTime: %v", err)
	}
	want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("next = %v, want %v (same day, not skipped)", got, want)
	}
}

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

func TestLocalStoreFlushesCreatedAncestors(t *testing.T) {
	root := t.TempDir()
	store, err := newLocalStore(root)
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	originalDir := fsyncDir
	t.Cleanup(func() { fsyncDir = originalDir })
	var synced []string
	fsyncDir = func(dir string) error {
		synced = append(synced, dir)
		return originalDir(dir)
	}

	key := "databases/" + uuid.NewString() + "/artifact.gz"
	if _, err := store.Put(context.Background(), key, bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	leaf := filepath.Dir(filepath.Join(root, filepath.FromSlash(key)))
	want := []string{leaf, filepath.Dir(leaf), root}
	for _, dir := range want {
		found := false
		for _, got := range synced {
			if got == dir {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("did not flush directory %s; flushed %v", dir, synced)
		}
	}
}

func TestNextRunTimeFallBackRecoveryDoesNotDoubleFire(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	served := time.Date(2025, 11, 2, 5, 30, 0, 0, time.UTC).In(loc)
	now := time.Date(2025, 11, 2, 6, 0, 0, 0, time.UTC).In(loc)
	next, err := nextRunTime("30 1 * * *", served, now, loc)
	if err != nil {
		t.Fatalf("nextRunTime: %v", err)
	}
	want := time.Date(2025, 11, 3, 1, 30, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v (the repeated slot must not fire twice)", next, want)
	}
	if second := time.Date(2025, 11, 2, 6, 30, 0, 0, time.UTC).In(loc); next.Equal(second) {
		t.Fatal("next re-scheduled the second occurrence of the served slot")
	}
}

func TestNextCronTimeWildcardHourSkipsRepeatedHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 11, 2, 5, 59, 0, 0, time.UTC).In(loc)
	next, err := nextCronTime("* * * * *", after, loc)
	if err != nil {
		t.Fatalf("nextCronTime: %v", err)
	}
	want := time.Date(2025, 11, 2, 7, 0, 0, 0, time.UTC).In(loc)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v (the second pass of 01:00-01:59 must be skipped)", next, want)
	}
}

func TestNextCronTimeSpringForwardEastOfUTC(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	after := time.Date(2025, 3, 30, 1, 0, 0, 0, loc)
	next, err := nextCronTime("30 2 * * *", after, loc)
	if err != nil {
		t.Fatalf("nextCronTime: %v", err)
	}
	want := time.Date(2025, 3, 30, 3, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v (the transition instant)", next, want)
	}
}

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

	if _, err := fixture.manager.UpdateTarget(ctx, fixture.userID, target.ID, TargetRequest{
		Name: "renamed", Prefix: strPtr("new-prefix/"),
	}); err != nil {
		t.Fatalf("name/prefix update: %v", err)
	}

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
	freshLists := fixture.containers.freshLists
	fixture.containers.mu.Unlock()
	if len(removes) != 1 || removes[0] != "leftover-backup" {
		t.Errorf("removed %v, want exactly [leftover-backup]", removes)
	}
	if freshLists == 0 {
		t.Error("the sweep used the cached List instead of ListFresh")
	}
}

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
