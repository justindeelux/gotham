package databases

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func TestBackupStoresCompressedArtifact(t *testing.T) {
	fixture := newBackupFixture(t)

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
	if backup.Type != BackupManual {
		t.Errorf("type = %q, want manual", backup.Type)
	}
	if !strings.HasPrefix(backup.Location, locationFilePrefix) {
		t.Errorf("location = %q, want a file:// uri", backup.Location)
	}
	if backup.Size <= 0 {
		t.Errorf("size = %d, want the compressed length", backup.Size)
	}
	if backup.FinishedAt.IsZero() {
		t.Error("finished_at must be set on a terminal run")
	}
	if backup.ContainerID == "" {
		t.Error("the temporary container id must be recorded")
	}

	// The stored bytes are the gzip of what the job streamed.
	if got := string(readArtifact(t, backup.Location)); got != dumpPayload {
		t.Errorf("artifact = %q, want %q", got, dumpPayload)
	}
	info, err := os.Stat(strings.TrimPrefix(backup.Location, locationFilePrefix))
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if info.Size() != backup.Size {
		t.Errorf("file is %d bytes, row says %d", info.Size(), backup.Size)
	}

	// The database was paused for the dump and started again afterwards, and
	// the job container was removed.
	fixture.containers.mu.Lock()
	stops, starts, runs, removes := fixture.containers.stops, fixture.containers.starts, len(fixture.containers.runs), fixture.containers.removes
	fixture.containers.mu.Unlock()
	if stops != 1 || starts != 1 {
		t.Errorf("stops = %d, starts = %d, want 1 and 1", stops, starts)
	}
	if runs != 1 {
		t.Errorf("started %d containers, want only the dump job", runs)
	}
	if len(removes) == 0 {
		t.Error("the temporary container must be removed")
	}
}

func TestBackupFailureIsRecordedAndDatabaseRestarted(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.containers.logFn = func(opts containers.RunOptions) [][]byte {
		runID := envValue(opts.Env, "GOTHAM_RUN_ID")
		return [][]byte{[]byte(
			jobStartPrefix + runID + "\n" +
				jobEndPrefix + runID + " fail 7\n" +
				"pg_dump: error: could not connect\n")}
	}

	backup := fixture.queueBackup(t)
	if backup.Status != BackupFailed {
		t.Fatalf("status = %q, want failed", backup.Status)
	}
	if backup.Location != "" {
		t.Errorf("a failed run must not record a location, got %q", backup.Location)
	}
	if !strings.Contains(backup.Error, "status 7") || !strings.Contains(backup.Error, "could not connect") {
		t.Errorf("error = %q, want the status and the diagnostics", backup.Error)
	}

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	fixture.containers.mu.Unlock()
	if starts != 1 {
		t.Errorf("starts = %d, want the database back up after the failure", starts)
	}
}

// TestBackupFailsWhenLogStreamBreaks is the U3 guard for the A3-5 CP fix: a
// terminal agent stream error must fail the backup instead of being treated as
// a clean dump.
func TestBackupFailsWhenLogStreamBreaks(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.containers.mu.Lock()
	fixture.containers.logStreamErr = errors.New("agent stream broke")
	fixture.containers.mu.Unlock()

	backup := fixture.queueBackup(t)
	if backup.Status != BackupFailed {
		t.Fatalf("status = %q (%s), want failed", backup.Status, backup.Error)
	}
	if !strings.Contains(backup.Error, "agent stream broke") {
		t.Errorf("error = %q, want the stream failure", backup.Error)
	}
}

func TestBackupLeavesStoppedDatabaseStopped(t *testing.T) {
	fixture := newBackupFixture(t)
	// No container listed: the database is stopped before the job runs.
	fixture.containers.listed = nil

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
	fixture.containers.mu.Lock()
	stops, starts := fixture.containers.stops, fixture.containers.starts
	fixture.containers.mu.Unlock()
	if stops != 0 || starts != 0 {
		t.Errorf("stops = %d, starts = %d, want neither: the database was already down", stops, starts)
	}
}

func TestBackupRejectsSecondJobWhileRunning(t *testing.T) {
	fixture := newBackupFixture(t)
	if !fixture.manager.claim(fixture.database.ID) {
		t.Fatal("could not take the claim")
	}
	defer fixture.manager.release(fixture.database.ID)

	_, err := fixture.manager.CreateBackup(context.Background(), fixture.userID, fixture.database.ID, CreateBackupRequest{})
	if !errors.Is(err, ErrBackupInFlight) {
		t.Fatalf("err = %v, want ErrBackupInFlight", err)
	}
}

func TestBackupEnforcesOwnership(t *testing.T) {
	fixture := newBackupFixture(t)
	stranger := uuid.New()

	if _, err := fixture.manager.CreateBackup(context.Background(), stranger, fixture.database.ID, CreateBackupRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateBackup err = %v, want ErrNotFound", err)
	}
	if _, err := fixture.manager.ListBackups(context.Background(), stranger, fixture.database.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListBackups err = %v, want ErrNotFound", err)
	}
	if _, err := fixture.manager.GetBackup(context.Background(), stranger, fixture.database.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBackup err = %v, want ErrNotFound", err)
	}
}

func TestBackupUsesObjectStoreSeam(t *testing.T) {
	databaseRepo := newFakeRepository()
	database := databaseRepo.seed(testDatabase(EnginePostgres, ""))
	containers := &fakeContainers{logFn: defaultJobLogs}
	containers.setRunning(database.ContainerID)
	objects := newFakeObjectStore()

	manager := NewBackupService(BackupConfig{
		Repository:         newFakeBackupRepository(),
		DatabaseRepository: databaseRepo,
		Containers:         containers,
		Secret:             testSecret,
		Logger:             discardLogger(),
		ObjectStore:        objects,
		JobTimeout:         30 * time.Second,
	})

	queued, err := manager.CreateBackup(context.Background(), database.UserID, database.ID, CreateBackupRequest{})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		backup, ok := manager.backups.(*fakeBackupRepository).getBackup(queued.ID)
		if ok && backup.Status != BackupRunning {
			if backup.Status != BackupCompleted {
				t.Fatalf("status = %q (%s)", backup.Status, backup.Error)
			}
			if !strings.HasPrefix(backup.Location, locationS3Prefix) {
				t.Errorf("location = %q, want an s3 uri", backup.Location)
			}
			wantKey := "databases/" + database.ID.String() + "/" + queued.ID.String() + ".dump.gz"
			if len(objects.puts) != 1 || objects.puts[0] != wantKey {
				t.Errorf("puts = %v, want [%s]", objects.puts, wantKey)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("backup never finished")
}

func TestRestoreStagesArtifactAndRunsJob(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)

	artifact, err := os.ReadFile(strings.TrimPrefix(backup.Location, locationFilePrefix))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: backup.ID})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if result.Status != BackupRunning || result.BackupID != backup.ID {
		t.Errorf("result = %+v, want the queued restore", result)
	}

	fixture.containers.mu.Lock()
	runsBefore := len(fixture.containers.runs)
	fixture.containers.mu.Unlock()
	_ = runsBefore

	waitFor(t, "the restore job", func() bool {
		fixture.containers.mu.Lock()
		defer fixture.containers.mu.Unlock()
		for _, opts := range fixture.containers.runs {
			if opts.Labels[labelRole] == roleRestore {
				return true
			}
		}
		return false
	})

	fixture.containers.mu.Lock()
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	stops, starts := fixture.containers.stops, fixture.containers.starts
	fixture.containers.mu.Unlock()

	var staged, restored *containers.RunOptions
	for i := range runs {
		switch runs[i].Labels[labelRole] {
		case roleStage:
			staged = &runs[i]
		case roleRestore:
			restored = &runs[i]
		}
	}
	if staged == nil {
		t.Fatal("no staging container ran")
	}
	if restored == nil {
		t.Fatal("no restore container ran")
	}

	// The staged chunk must carry the artifact byte for byte.
	pattern := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)'`)
	match := pattern.FindStringSubmatch(staged.Command[2])
	if match == nil {
		t.Fatalf("staging script has no payload: %s", staged.Command[2])
	}
	decoded, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatalf("decode staged chunk: %v", err)
	}
	if !bytes.Equal(decoded, artifact) {
		t.Errorf("staged %d bytes, artifact is %d bytes (or they differ)", len(decoded), len(artifact))
	}

	// The restore job mounts the volume and knows where the artifact is.
	if want := fixture.database.StoragePath + ":/var/lib/postgresql/data"; len(restored.Volumes) != 1 || restored.Volumes[0] != want {
		t.Errorf("volumes = %v, want [%s]", restored.Volumes, want)
	}
	if stagedPath := envValue(restored.Env, "GOTHAM_STAGED"); !strings.Contains(stagedPath, stagingDirName) {
		t.Errorf("GOTHAM_STAGED = %q, want a path inside %s", stagedPath, stagingDirName)
	}
	if stops < 1 || starts < 1 {
		t.Errorf("stops = %d, starts = %d, want the database paused and resumed", stops, starts)
	}
}

// TestBackupPG18MountPath is the D1-7 backup/restore regression: the dump and
// restore job containers (and the staging directory) must use the same
// versioned mount the live container uses, or a PostgreSQL 18 job runs the
// entrypoint against /var/lib/postgresql/data and refuses to start.
func TestBackupPG18MountPath(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "postgres16", version: "16-alpine", want: postgresLegacyMountPath},
		{name: "postgres18", version: "18-alpine", want: postgres18MountPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDatabase(EnginePostgres, tt.version)
			credentials := Credentials{Username: "app", Password: "pw", Database: "appdb"}

			opts, err := backupJobOptions(db, credentials, "gotham-backup-abc", roleBackup, "run-1", "", "true")
			if err != nil {
				t.Fatalf("backupJobOptions: %v", err)
			}
			want := db.StoragePath + ":" + tt.want
			if len(opts.Volumes) != 1 || opts.Volumes[0] != want {
				t.Errorf("backup volumes = %v, want [%s]", opts.Volumes, want)
			}

			dir, err := stagingDir(db)
			if err != nil {
				t.Fatalf("stagingDir: %v", err)
			}
			if wantDir := strings.TrimRight(tt.want, "/") + "/" + stagingDirName; dir != wantDir {
				t.Errorf("stagingDir = %q, want %q", dir, wantDir)
			}
		})
	}
}

// TestPG18StageAndCleanupMounts pins the two job paths a partial revert misses:
// stageChunk (the restore staging container) and removeStagedArtifact (the
// post-failure cleanup container) must both use the versioned mount for
// PostgreSQL 18, not only backupJobOptions/stagingDir.
func TestPG18StageAndCleanupMounts(t *testing.T) {
	fixture := newBackupFixture(t)
	database := fixture.database
	database.Version = "18-alpine"
	want := database.StoragePath + ":" + postgres18MountPath
	staged := postgres18MountPath + "/" + stagingDirName + "/" + database.ID.String() + ".part"

	if err := fixture.manager.stageChunk(context.Background(), database, database.ID, []byte("chunk-0"), staged, 0); err != nil {
		t.Fatalf("stageChunk: %v", err)
	}
	fixture.manager.removeStagedArtifact(database, staged)

	fixture.containers.mu.Lock()
	runs := append([]containers.RunOptions(nil), fixture.containers.runs...)
	fixture.containers.mu.Unlock()
	if len(runs) != 2 {
		t.Fatalf("job runs = %d, want 2 (stage + cleanup)", len(runs))
	}
	for i, run := range runs {
		if len(run.Volumes) != 1 || run.Volumes[0] != want {
			t.Errorf("run %d volumes = %v, want [%s]", i, run.Volumes, want)
		}
	}
}

func TestRestoreRejectsIncompleteBackup(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.backups.seedBackup(Backup{
		DatabaseID: fixture.database.ID,
		Status:     BackupFailed,
		Location:   locationFilePrefix + "/tmp/missing.gz",
	})
	_, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: backup.ID})
	if !errors.Is(err, ErrBackupNotCompleted) {
		t.Fatalf("err = %v, want ErrBackupNotCompleted", err)
	}
}

func TestRestoreEnforcesOwnership(t *testing.T) {
	fixture := newBackupFixture(t)
	other := fixture.backups.seedBackup(Backup{DatabaseID: fixture.database.ID, Status: BackupCompleted})
	stranger := uuid.New()

	if _, err := fixture.manager.RestoreBackup(context.Background(), stranger, fixture.database.ID,
		RestoreRequest{BackupID: other.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign caller err = %v, want ErrNotFound", err)
	}
	if _, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
		RestoreRequest{}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing backup id err = %v, want ErrValidation", err)
	}

	// A backup that belongs to another database is invisible here.
	foreign := fixture.backups.seedBackup(Backup{DatabaseID: uuid.New(), Status: BackupCompleted})
	if _, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, fixture.database.ID,
		RestoreRequest{BackupID: foreign.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-database err = %v, want ErrNotFound", err)
	}
}

func TestDeleteBackupRemovesArtifactAndRow(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)
	path := strings.TrimPrefix(backup.Location, locationFilePrefix)

	if err := fixture.manager.DeleteBackup(context.Background(), fixture.userID, fixture.database.ID, backup.ID); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("artifact still on disk: %v", err)
	}
	if _, ok := fixture.backups.getBackup(backup.ID); ok {
		t.Error("row still present after delete")
	}
	if err := fixture.manager.DeleteBackup(context.Background(), fixture.userID, fixture.database.ID, backup.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestTargetCredentialsAreSealed(t *testing.T) {
	fixture := newBackupFixture(t)

	target, err := fixture.manager.CreateTarget(context.Background(), fixture.userID, TargetRequest{
		Name:      "r2",
		Kind:      "s3",
		Endpoint:  "https://account.r2.cloudflarestorage.com",
		Region:    "auto",
		Bucket:    "gotham-backups",
		Prefix:    "pg-orders/",
		AccessKey: "R2AK7f3c9a2b51de84",
		SecretKey: "b7d41e90c3f5a28e6d0194bc7a3e52f0",
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}

	secrets := fixture.backups.getSecrets(target.ID)
	if len(secrets) != 2 {
		t.Fatalf("stored %d secrets, want access and secret key", len(secrets))
	}
	for _, secret := range secrets {
		if secret.Ciphertext == "" {
			t.Errorf("%s stored empty", secret.Key)
		}
		if strings.Contains(secret.Ciphertext, "R2AK7f3c9a2b51de84") ||
			strings.Contains(secret.Ciphertext, "b7d41e90c3f5a28e6d0194bc7a3e52f0") {
			t.Errorf("%s is stored in the clear", secret.Key)
		}
	}

	// The sealed rows open back to the original values.
	accessKey, secretKey, err := openTargetSecrets(testSecret, secrets)
	if err != nil {
		t.Fatalf("openTargetSecrets: %v", err)
	}
	if accessKey != "R2AK7f3c9a2b51de84" || secretKey != "b7d41e90c3f5a28e6d0194bc7a3e52f0" {
		t.Errorf("opened %q / %q", accessKey, secretKey)
	}

	// A request without credentials keeps the stored ones.
	if _, err := fixture.manager.UpdateTarget(context.Background(), fixture.userID, target.ID, TargetRequest{
		Name: "r2-renamed",
	}); err != nil {
		t.Fatalf("UpdateTarget: %v", err)
	}
	accessKey, _, err = openTargetSecrets(testSecret, fixture.backups.getSecrets(target.ID))
	if err != nil || accessKey != "R2AK7f3c9a2b51de84" {
		t.Errorf("stored access key was lost: %q, %v", accessKey, err)
	}
}

func TestTargetOwnershipAndValidation(t *testing.T) {
	fixture := newBackupFixture(t)
	stranger := uuid.New()

	if _, err := fixture.manager.CreateTarget(context.Background(), uuid.Nil, TargetRequest{Name: "x", Kind: "local"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("nil user err = %v, want ErrNotFound", err)
	}
	if _, err := fixture.manager.CreateTarget(context.Background(), fixture.userID, TargetRequest{Name: "", Kind: "s3"}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing name err = %v, want ErrValidation", err)
	}
	if _, err := fixture.manager.CreateTarget(context.Background(), fixture.userID, TargetRequest{
		Name: "s3", Kind: "s3", Endpoint: "", Bucket: "",
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing endpoint err = %v, want ErrValidation", err)
	}
	if _, err := fixture.manager.CreateTarget(context.Background(), fixture.userID, TargetRequest{
		Name: "weird", Kind: "ftp",
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("bad kind err = %v, want ErrValidation", err)
	}

	target := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "mine", Kind: TargetS3,
		Endpoint: "http://minio:9000", Bucket: "b"})
	if _, err := fixture.manager.UpdateTarget(context.Background(), stranger, target.ID, TargetRequest{Name: "stolen"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign update err = %v, want ErrNotFound", err)
	}
	if err := fixture.manager.DeleteTarget(context.Background(), stranger, target.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign delete err = %v, want ErrNotFound", err)
	}
}

func TestBackupNeverLogsCredentials(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fixture := newBackupFixtureWith(t, logger)

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s)", backup.Status, backup.Error)
	}

	output := logged.String()
	credentials := testCredentials()
	for _, secret := range []string{credentials.Password, credentials.RootPassword, credentials.Username} {
		if secret != "" && strings.Contains(output, secret) {
			t.Errorf("credentials leaked into the log: %q", secret)
		}
	}
	// The job container carries the password in its environment — that is
	// where the live database keeps it too — but the payload itself must be
	// described without it.
	if strings.Contains(output, "POSTGRES_PASSWORD") {
		t.Error("the log must not echo the job environment")
	}
}

func TestSchedulesLifecycle(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()

	if _, err := fixture.manager.CreateSchedule(ctx, fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron: "not a cron",
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid cron err = %v, want ErrValidation", err)
	}

	enabled := false
	schedule, err := fixture.manager.CreateSchedule(ctx, fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron:    "0 2 * * *",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if schedule.NextRunAt.IsZero() || schedule.NextRunAt.Before(time.Now()) {
		t.Errorf("next_run_at = %v, want a future instant", schedule.NextRunAt)
	}
	if schedule.Enabled {
		t.Error("enabled = true, want the requested false")
	}

	enabled = true
	updated, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron:    "0 */6 * * *",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if updated.Cron != "0 */6 * * *" || !updated.Enabled {
		t.Errorf("schedule = %+v, want the new cron and enabled", updated)
	}

	list, err := fixture.manager.ListSchedules(ctx, fixture.userID, fixture.database.ID)
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d schedules, want 1", len(list))
	}

	// A schedule of another database is not ours to delete.
	stranger := uuid.New()
	if err := fixture.manager.DeleteSchedule(ctx, stranger, fixture.database.ID, schedule.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign delete err = %v, want ErrNotFound", err)
	}
	if err := fixture.manager.DeleteSchedule(ctx, fixture.userID, fixture.database.ID, schedule.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	if _, err := fixture.manager.ListSchedules(ctx, fixture.userID, fixture.database.ID); err != nil {
		t.Fatalf("ListSchedules after delete: %v", err)
	}
}

// TestScheduleUpdateTargetClearSemantics proves the tri-state target update:
// an absent target_id keeps the stored target, an explicit empty string
// clears it (runs fall back to the local directory) and an id replaces it.
func TestScheduleUpdateTargetClearSemantics(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	s3 := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "s3", Kind: TargetS3,
		Endpoint: "https://s3.example.com", Bucket: "b"})
	local := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "local", Kind: TargetLocal})

	s3ID := s3.ID.String()
	schedule, err := fixture.manager.CreateSchedule(ctx, fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron:     "0 2 * * *",
		TargetID: &s3ID,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if schedule.TargetID != s3.ID {
		t.Fatalf("created target = %s, want %s", schedule.TargetID, s3.ID)
	}

	// Absent field: the stored target survives.
	kept, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron: "0 3 * * *",
	})
	if err != nil {
		t.Fatalf("update without target_id: %v", err)
	}
	if kept.TargetID != s3.ID {
		t.Errorf("target = %s, want the stored %s (absent must not clear)", kept.TargetID, s3.ID)
	}

	// Explicit empty string: the target is cleared.
	clear := ""
	cleared, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron:     "0 4 * * *",
		TargetID: &clear,
	})
	if err != nil {
		t.Fatalf("clear update: %v", err)
	}
	if cleared.TargetID != uuid.Nil {
		t.Errorf("target = %s, want cleared to no target", cleared.TargetID)
	}

	// Explicit id: the target is replaced.
	localID := local.ID.String()
	replaced, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron:     "0 5 * * *",
		TargetID: &localID,
	})
	if err != nil {
		t.Fatalf("replace update: %v", err)
	}
	if replaced.TargetID != local.ID {
		t.Errorf("target = %s, want %s", replaced.TargetID, local.ID)
	}

	// An id the caller does not own is rejected and the stored target stays.
	foreign := fixture.backups.seedTarget(BackupTarget{UserID: uuid.New(), Name: "foreign", Kind: TargetLocal})
	foreignID := foreign.ID.String()
	if _, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron:     "0 6 * * *",
		TargetID: &foreignID,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign target err = %v, want ErrNotFound", err)
	}
}

func TestBackupManagerWithoutRepositoryFailsClearly(t *testing.T) {
	manager := NewBackupService(BackupConfig{Logger: discardLogger()})
	if _, err := manager.ListTargets(context.Background(), uuid.New()); err == nil {
		t.Error("expected an error without a repository")
	}
	if _, err := manager.ListBackups(context.Background(), uuid.New(), uuid.New(), 0); err == nil {
		t.Error("expected an error without a repository")
	}
	if _, err := manager.CreateBackup(context.Background(), uuid.New(), uuid.New(), CreateBackupRequest{}); err == nil {
		t.Error("expected an error without a repository")
	}
}

// TestReconcileStaleBackups pins the crash recovery contract: a run left
// running by a previous control plane process is swept to failed and its
// database container is resumed.
func TestReconcileStaleBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	stale, err := fixture.backups.CreateBackup(context.Background(), Backup{
		DatabaseID: fixture.database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed running backup: %v", err)
	}

	fixture.manager.reconcileStaleBackups()

	got, err := fixture.backups.GetBackup(context.Background(), stale.ID)
	if err != nil {
		t.Fatalf("GetBackup: %v", err)
	}
	if got.Status != BackupFailed {
		t.Errorf("status = %q, want %q", got.Status, BackupFailed)
	}
	if got.Error == "" {
		t.Error("swept backup carries no error")
	}
	if got.FinishedAt.IsZero() {
		t.Error("swept backup has no finished_at")
	}

	fixture.containers.mu.Lock()
	starts := fixture.containers.starts
	fixture.containers.mu.Unlock()
	if starts == 0 {
		t.Error("database container was not resumed after the sweep")
	}
}

// TestStageChunkFitsArgumentLimit pins Linux's per-argument limit
// (MAX_ARG_STRLEN = 128 KiB): the base64 chunk plus script overhead must stay
// under it, or restore fails with E2BIG for any artifact larger than ~97 KB.
func TestStageChunkFitsArgumentLimit(t *testing.T) {
	const maxArgStrlen = 128 * 1024
	const scriptOverhead = 4096
	encoded := (stageChunkBytes/3 + 1) * 4
	if encoded+scriptOverhead >= maxArgStrlen {
		t.Fatalf("chunk encodes to %d bytes; with %d overhead it must stay under %d",
			encoded, scriptOverhead, maxArgStrlen)
	}
}

// TestStageChunkDecodesBase64IntoStagedFile guards the staging contract: the
// command argument carries base64, but the file the restore job reads must be
// the decoded artifact bytes. Truncate on the first chunk, append after it.
func TestStageChunkDecodesBase64IntoStagedFile(t *testing.T) {
	fixture := newBackupFixture(t)
	runID := fixture.database.ID
	fixture.containers.logFn = func(containers.RunOptions) [][]byte {
		return [][]byte{[]byte(
			jobStartPrefix + runID.String() + "\n" +
				jobPayloadPrefix + runID.String() + " 0\n" +
				jobEndPrefix + runID.String() + " ok\n")}
	}
	staged := "/var/lib/postgresql/data/.gotham-restore/x.part"
	if err := fixture.manager.stageChunk(context.Background(), fixture.database, runID, []byte("chunk-0"), staged, 0); err != nil {
		t.Fatalf("stageChunk index 0: %v", err)
	}
	if err := fixture.manager.stageChunk(context.Background(), fixture.database, runID, []byte("chunk-1"), staged, 1); err != nil {
		t.Fatalf("stageChunk index 1: %v", err)
	}
	if len(fixture.containers.runs) != 2 {
		t.Fatalf("staging runs = %d, want 2", len(fixture.containers.runs))
	}
	first, second := fixture.containers.runs[0].Command[2], fixture.containers.runs[1].Command[2]
	if !strings.Contains(first, "base64 -d > '"+staged+"'") {
		t.Errorf("first chunk does not decode into the staged file:\n%s", first)
	}
	if !strings.Contains(second, "base64 -d >> '"+staged+"'") {
		t.Errorf("later chunks do not decode with append:\n%s", second)
	}
	if !strings.Contains(first, base64.StdEncoding.EncodeToString([]byte("chunk-0"))) {
		t.Error("first chunk script does not carry the encoded bytes")
	}
}

// TestCreateTargetS3RequiresCredentials guards the target API: an s3 target
// missing either key is rejected at create time instead of failing on the
// first backup run.
func TestCreateTargetS3RequiresCredentials(t *testing.T) {
	fixture := newBackupFixture(t)
	for _, req := range []TargetRequest{
		{Name: "no-keys", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b"},
		{Name: "access-only", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak"},
		{Name: "secret-only", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", SecretKey: "sk"},
	} {
		if _, err := fixture.manager.CreateTarget(context.Background(), fixture.userID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("CreateTarget(%s) err = %v, want ErrValidation", req.Name, err)
		}
	}
}

// TestUpdateTargetS3SwitchRequiresCredentials proves the update path has the
// same credential requirement as the create path: switching a target to s3
// without a complete pair is rejected and leaves the row untouched, while a
// request that carries both halves switches and seals them.
func TestUpdateTargetS3SwitchRequiresCredentials(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "local", Kind: TargetLocal})

	rejected := []TargetRequest{
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b"},
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak"},
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", SecretKey: "sk"},
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak", SecretKey: "   "},
	}
	for _, req := range rejected {
		if _, err := fixture.manager.UpdateTarget(ctx, fixture.userID, target.ID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("UpdateTarget(%+v) err = %v, want ErrValidation", req, err)
		}
	}
	stored, err := fixture.backups.GetBackupTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if stored.Kind != TargetLocal {
		t.Fatalf("kind = %q, want the rejected switch to leave the target local", stored.Kind)
	}

	switched, err := fixture.manager.UpdateTarget(ctx, fixture.userID, target.ID, TargetRequest{
		Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("UpdateTarget with credentials: %v", err)
	}
	if switched.Kind != TargetS3 {
		t.Fatalf("kind = %q, want s3", switched.Kind)
	}
	if secrets := fixture.backups.getSecrets(target.ID); len(secrets) != 2 {
		t.Fatalf("stored %d secrets, want the supplied pair", len(secrets))
	}
}

// TestUpdateTargetS3FieldsWithoutCredentialsKeepsStoredKeys proves a
// non-credential update of an s3 target still works when the pair is already
// stored, and that the stored keys survive it.
func TestUpdateTargetS3FieldsWithoutCredentialsKeepsStoredKeys(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	created, err := fixture.manager.CreateTarget(ctx, fixture.userID, TargetRequest{
		Name: "r2", Kind: "s3", Endpoint: "https://account.r2.cloudflarestorage.com",
		Bucket: "gotham-backups", AccessKey: "R2AK7f3c9a2b51de84", SecretKey: "b7d41e90c3f5a28e6d0194bc7a3e52f0",
	})
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}

	updated, err := fixture.manager.UpdateTarget(ctx, fixture.userID, created.ID, TargetRequest{
		Endpoint: "https://account2.r2.cloudflarestorage.com",
		Bucket:   "gotham-backups-v2",
		Prefix:   "pg-orders/",
	})
	if err != nil {
		t.Fatalf("credential-less s3 update: %v", err)
	}
	if updated.Endpoint != "https://account2.r2.cloudflarestorage.com" ||
		updated.Bucket != "gotham-backups-v2" || updated.Prefix != "pg-orders/" {
		t.Errorf("updated = %+v, want the new endpoint, bucket and prefix", updated)
	}
	accessKey, secretKey, err := openTargetSecrets(testSecret, fixture.backups.getSecrets(created.ID))
	if err != nil {
		t.Fatalf("openTargetSecrets: %v", err)
	}
	if accessKey != "R2AK7f3c9a2b51de84" || secretKey != "b7d41e90c3f5a28e6d0194bc7a3e52f0" {
		t.Errorf("stored keys changed: %q / %q", accessKey, secretKey)
	}
}
