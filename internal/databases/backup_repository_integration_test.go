package databases

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestBackupRepositoryRoundTrip exercises the SQL behind the backup
// repository against a real PostgreSQL: the join that hides backups of a
// deleted database, the due-time query the scheduler runs, the unique target
// name and the upsert of sealed credentials. It skips when no database is
// reachable, so CI stays green without one.
func TestBackupRepositoryRoundTrip(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)

	now := time.Now().UTC()
	databaseRepo := newStoreRepository(st)
	database, err := databaseRepo.CreateDatabase(ctx, Database{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "backup-target",
		Engine:      EnginePostgres,
		Version:     "16-alpine",
		Status:      StatusRunning,
		StoragePath: "gotham-db-" + uuid.New().String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	// --- backups ---------------------------------------------------------
	backup, err := repo.CreateBackup(ctx, Backup{
		ID:         uuid.New(),
		DatabaseID: database.ID,
		Type:       BackupManual,
		Status:     BackupRunning,
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if backup.Status != BackupRunning || backup.Type != BackupManual {
		t.Errorf("backup = %+v", backup)
	}

	listed, err := repo.ListBackupsByDatabase(ctx, database.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListBackupsByDatabase = %d rows (%v), want 1", len(listed), err)
	}

	finished, err := repo.FinishBackup(ctx, Backup{
		ID:          backup.ID,
		Status:      BackupCompleted,
		Size:        1024,
		Location:    locationFilePrefix + "/tmp/backup.gz",
		ContainerID: "gotham-backup-abcd1234",
		FinishedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("FinishBackup: %v", err)
	}
	if finished.Status != BackupCompleted || finished.Size != 1024 ||
		finished.Location == "" || finished.ContainerID == "" || finished.FinishedAt.IsZero() {
		t.Errorf("finished = %+v", finished)
	}

	if _, err := repo.GetBackup(ctx, uuid.New()); err != ErrNotFound {
		t.Errorf("GetBackup(missing) err = %v, want ErrNotFound", err)
	}

	// --- schedules -------------------------------------------------------
	late, err := repo.CreateBackupSchedule(ctx, BackupSchedule{
		ID:         uuid.New(),
		DatabaseID: database.ID,
		Cron:       "0 2 * * *",
		Enabled:    true,
		NextRunAt:  now.Add(-time.Minute),
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatalf("CreateBackupSchedule: %v", err)
	}
	if _, err := repo.CreateBackupSchedule(ctx, BackupSchedule{
		ID:         uuid.New(),
		DatabaseID: database.ID,
		Cron:       "0 4 * * *",
		Enabled:    false,
		NextRunAt:  now.Add(-time.Minute),
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("CreateBackupSchedule(disabled): %v", err)
	}

	due, err := repo.ListDueBackupSchedules(ctx, now)
	if err != nil {
		t.Fatalf("ListDueBackupSchedules: %v", err)
	}
	dueIDs := make(map[uuid.UUID]bool, len(due))
	for _, schedule := range due {
		dueIDs[schedule.ID] = true
	}
	if !dueIDs[late.ID] {
		t.Errorf("the enabled schedule is not due: %+v", due)
	}
	for _, schedule := range due {
		if schedule.ID != late.ID && schedule.DatabaseID == database.ID {
			t.Errorf("a disabled schedule is due: %+v", schedule)
		}
	}

	next := now.Add(time.Hour)
	advanced, err := repo.MarkBackupScheduleRun(ctx, late.ID, now, next)
	if err != nil {
		t.Fatalf("MarkBackupScheduleRun: %v", err)
	}
	if advanced.LastRunAt.IsZero() || !advanced.NextRunAt.Equal(next) {
		t.Errorf("advanced = %+v", advanced)
	}
	if due, err = repo.ListDueBackupSchedules(ctx, now); err != nil {
		t.Fatalf("ListDueBackupSchedules: %v", err)
	}
	for _, schedule := range due {
		if schedule.ID == late.ID {
			t.Error("an advanced schedule must not be due any more")
		}
	}

	// --- targets and sealed credentials ----------------------------------
	target, err := repo.CreateBackupTarget(ctx, BackupTarget{
		ID:       uuid.New(),
		UserID:   ownerID,
		Name:     "minio-" + uuid.New().String()[:8],
		Kind:     TargetS3,
		Endpoint: "http://minio:9000",
		Region:   "us-east-1",
		Bucket:   "gotham-backups",
		Prefix:   "pg/",
	})
	if err != nil {
		t.Fatalf("CreateBackupTarget: %v", err)
	}
	if _, err := repo.CreateBackupTarget(ctx, BackupTarget{
		ID: uuid.New(), UserID: ownerID, Name: target.Name, Kind: TargetLocal,
	}); err != ErrConflict {
		t.Errorf("duplicate target name err = %v, want ErrConflict", err)
	}

	targets, err := repo.ListBackupTargetsByUser(ctx, ownerID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("ListBackupTargetsByUser = %d (%v), want 1", len(targets), err)
	}

	if _, err := repo.UpsertTargetSecret(ctx, TargetSecret{TargetID: target.ID, Key: targetSecretAccessKey, Ciphertext: "sealed-1"}); err != nil {
		t.Fatalf("UpsertTargetSecret: %v", err)
	}
	if _, err := repo.UpsertTargetSecret(ctx, TargetSecret{TargetID: target.ID, Key: targetSecretAccessKey, Ciphertext: "sealed-2"}); err != nil {
		t.Fatalf("UpsertTargetSecret(replace): %v", err)
	}
	secrets, err := repo.ListTargetSecrets(ctx, target.ID)
	if err != nil || len(secrets) != 1 {
		t.Fatalf("ListTargetSecrets = %d (%v), want the upserted row only", len(secrets), err)
	}
	if secrets[0].Ciphertext != "sealed-2" {
		t.Errorf("ciphertext = %q, want the replaced value", secrets[0].Ciphertext)
	}

	// --- ownership and cascade -------------------------------------------
	if _, err := repo.DeleteBackupTarget(ctx, target.ID, uuid.New()); err != ErrNotFound {
		t.Errorf("foreign DeleteBackupTarget err = %v, want ErrNotFound", err)
	}
	if _, err := repo.DeleteBackupTarget(ctx, target.ID, ownerID); err != nil {
		t.Fatalf("DeleteBackupTarget: %v", err)
	}

	// Soft-deleting the database hides its backups and schedules: the queries
	// join on a live row, which is what keeps a deleted database's history
	// out of every read.
	if _, err := databaseRepo.SoftDeleteDatabase(ctx, database.ID); err != nil {
		t.Fatalf("SoftDeleteDatabase: %v", err)
	}
	if _, err := repo.GetBackup(ctx, backup.ID); err != ErrNotFound {
		t.Errorf("GetBackup after soft delete err = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetBackupSchedule(ctx, late.ID); err != ErrNotFound {
		t.Errorf("GetBackupSchedule after soft delete err = %v, want ErrNotFound", err)
	}
}
