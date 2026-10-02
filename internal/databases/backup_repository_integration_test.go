package databases

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
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

	now := time.Now().UTC().Truncate(time.Microsecond)
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

	listed, err := repo.ListBackupsByDatabase(ctx, database.ID, maxBackupListLimit)
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

// TestBackupTargetWriteIsAtomic is the D2-11 regression against a real
// PostgreSQL: a failing half of the write must roll the other half back, so no
// mixed configuration/credential pair is observable.
func TestBackupTargetWriteIsAtomic(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, _ := seedUserAndServer(t, st)

	suffix := uuid.New().String()[:8]
	firstName := "atomic-a-" + suffix
	targetID := uuid.New()
	if _, err := repo.CreateBackupTargetWithSecrets(ctx, BackupTarget{
		ID: targetID, UserID: ownerID, Name: firstName,
		Kind: TargetS3, Endpoint: "https://old", Bucket: "old",
	}, []TargetSecret{
		{TargetID: targetID, Key: targetSecretAccessKey, Ciphertext: "old-access"},
		{TargetID: targetID, Key: targetSecretSecretKey, Ciphertext: "old-secret"},
	}); err != nil {
		t.Fatalf("CreateBackupTargetWithSecrets: %v", err)
	}
	secondName := "atomic-b-" + suffix
	if _, err := repo.CreateBackupTargetWithSecrets(ctx, BackupTarget{
		ID: uuid.New(), UserID: ownerID, Name: secondName, Kind: TargetLocal,
	}, nil); err != nil {
		t.Fatalf("CreateBackupTargetWithSecrets(second): %v", err)
	}

	// A config failure (duplicate name) must not leave the new credentials.
	if _, err := repo.UpdateBackupTargetWithSecrets(ctx, BackupTarget{
		ID: targetID, Name: secondName, Kind: TargetS3, Endpoint: "https://moved", Bucket: "moved",
	}, []TargetSecret{
		{TargetID: targetID, Key: targetSecretAccessKey, Ciphertext: "new-access"},
	}); err == nil {
		t.Fatal("expected the duplicate name to fail the transaction")
	}
	assertTargetState(t, repo, ctx, targetID, firstName, "https://old", "old")
	assertSecret(t, repo, ctx, targetID, targetSecretAccessKey, "old-access")

	// A credential failure (foreign target) must not leave the new config.
	if _, err := repo.UpdateBackupTargetWithSecrets(ctx, BackupTarget{
		ID: targetID, Name: firstName, Kind: TargetS3, Endpoint: "https://moved", Bucket: "moved",
	}, []TargetSecret{
		{TargetID: targetID, Key: targetSecretAccessKey, Ciphertext: "newer-access"},
		{TargetID: uuid.New(), Key: targetSecretSecretKey, Ciphertext: "bad-fk"},
	}); err == nil {
		t.Fatal("expected the foreign secret to fail the transaction")
	}
	assertTargetState(t, repo, ctx, targetID, firstName, "https://old", "old")
	assertSecret(t, repo, ctx, targetID, targetSecretAccessKey, "old-access")
}

// assertTargetState checks the stored configuration of a target.
func assertTargetState(t *testing.T, repo *storeBackupRepository, ctx context.Context, targetID uuid.UUID, name, endpoint, bucket string) {
	t.Helper()
	got, err := repo.GetBackupTarget(ctx, targetID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if got.Name != name || got.Endpoint != endpoint || got.Bucket != bucket {
		t.Errorf("target = (%q, %q, %q), want (%q, %q, %q)", got.Name, got.Endpoint, got.Bucket, name, endpoint, bucket)
	}
}

// assertSecret checks one stored sealed credential's ciphertext.
func assertSecret(t *testing.T, repo *storeBackupRepository, ctx context.Context, targetID uuid.UUID, key, ciphertext string) {
	t.Helper()
	secrets, err := repo.ListTargetSecrets(ctx, targetID)
	if err != nil {
		t.Fatalf("ListTargetSecrets: %v", err)
	}
	for _, secret := range secrets {
		if secret.Key == key {
			if secret.Ciphertext != ciphertext {
				t.Errorf("secret %s = %q, want %q", key, secret.Ciphertext, ciphertext)
			}
			return
		}
	}
	t.Errorf("secret %s is missing", key)
}

// TestListBackupsByDatabaseRespectsLimit is the D2-14 regression: the query
// must never return more rows than the requested bound.
func TestListBackupsByDatabaseRespectsLimit(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)

	database, err := newStoreRepository(st).CreateDatabase(ctx, Database{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "backup-limit",
		Engine:      EnginePostgres,
		Version:     "16-alpine",
		Status:      StatusRunning,
		StoragePath: "gotham-db-" + uuid.New().String(),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := repo.CreateBackup(ctx, Backup{
			ID: uuid.New(), DatabaseID: database.ID,
			Type: BackupManual, Status: BackupCompleted,
		}); err != nil {
			t.Fatalf("CreateBackup: %v", err)
		}
	}
	limited, err := repo.ListBackupsByDatabase(ctx, database.ID, 2)
	if err != nil {
		t.Fatalf("ListBackupsByDatabase: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limited list = %d rows, want 2", len(limited))
	}
	all, err := repo.ListBackupsByDatabase(ctx, database.ID, maxBackupListLimit)
	if err != nil {
		t.Fatalf("ListBackupsByDatabase(all): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("full list = %d rows, want 3", len(all))
	}
}

// TestHasBackupsForTargetCountsLiveRuns exercises the destination lock query:
// a running run locks the target (it will record the old destination), and a
// target with no live or completed run does not.
func TestHasBackupsForTargetCountsLiveRuns(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)

	targetID := uuid.New()
	if _, err := repo.CreateBackupTargetWithSecrets(ctx, BackupTarget{
		ID: targetID, UserID: ownerID, Name: "lock-" + uuid.New().String()[:8], Kind: TargetLocal,
	}, nil); err != nil {
		t.Fatalf("CreateBackupTargetWithSecrets: %v", err)
	}
	if has, err := repo.HasBackupsForTarget(ctx, targetID); err != nil || has {
		t.Fatalf("empty target has = %v (%v), want false", has, err)
	}

	database, err := newStoreRepository(st).CreateDatabase(ctx, Database{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "backup-lock",
		Engine:      EnginePostgres,
		Version:     "16-alpine",
		Status:      StatusRunning,
		StoragePath: "gotham-db-" + uuid.New().String(),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	running, err := repo.CreateBackup(ctx, Backup{
		ID: uuid.New(), DatabaseID: database.ID, TargetID: targetID,
		Type: BackupManual, Status: BackupRunning,
	})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if has, err := repo.HasBackupsForTarget(ctx, targetID); err != nil || !has {
		t.Fatalf("running target has = %v (%v), want true", has, err)
	}
	if _, err := repo.FinishBackup(ctx, Backup{
		ID: running.ID, Status: BackupFailed, FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("FinishBackup: %v", err)
	}
	if has, err := repo.HasBackupsForTarget(ctx, targetID); err != nil || has {
		t.Fatalf("failed-only target has = %v (%v), want false", has, err)
	}
}

// seedRaceTarget creates a target and a database for the lock interleaving
// tests and returns them.
func seedRaceTarget(t *testing.T, st *store.Store, repo *storeBackupRepository, ownerID, serverID uuid.UUID) (BackupTarget, Database) {
	t.Helper()
	ctx := context.Background()
	targetID := uuid.New()
	target, err := repo.CreateBackupTargetWithSecrets(ctx, BackupTarget{
		ID: targetID, UserID: ownerID, Name: "race-" + uuid.New().String()[:8],
		Kind: TargetS3, Endpoint: "https://old", Bucket: "old",
	}, []TargetSecret{{TargetID: targetID, Key: targetSecretAccessKey, Ciphertext: "sealed"}})
	if err != nil {
		t.Fatalf("CreateBackupTargetWithSecrets: %v", err)
	}
	database, err := newStoreRepository(st).CreateDatabase(ctx, Database{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "race-db-" + uuid.New().String()[:8],
		Engine:      EnginePostgres,
		Version:     "16-alpine",
		Status:      StatusRunning,
		StoragePath: "gotham-db-" + uuid.New().String(),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	return target, database
}

// TestBackupRunWaitsForDestinationEditLock is the U3 regression, edit-first
// direction: while a destination edit holds the target row, a run that
// references the target blocks, and after the edit commits the run carries the
// post-edit configuration.
func TestBackupRunWaitsForDestinationEditLock(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	target, database := seedRaceTarget(t, st, repo, ownerID, serverID)

	tx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT id FROM backup_targets WHERE id = $1 FOR UPDATE", target.ID); err != nil {
		t.Fatalf("lock target: %v", err)
	}

	type runResult struct {
		target *BackupTarget
		err    error
	}
	done := make(chan runResult, 1)
	go func() {
		_, live, err := repo.CreateBackupWithTarget(ctx, Backup{
			ID: uuid.New(), DatabaseID: database.ID, TargetID: target.ID,
			Type: BackupManual, Status: BackupRunning,
		}, nil)
		done <- runResult{target: live, err: err}
	}()

	select {
	case res := <-done:
		t.Fatalf("run did not wait for the destination edit lock: %+v", res)
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := tx.Exec(ctx, "UPDATE backup_targets SET endpoint = 'https://new', bucket = 'new' WHERE id = $1", target.ID); err != nil {
		t.Fatalf("move target: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("run after edit: %v", res.err)
	}
	if res.target == nil || res.target.Endpoint != "https://new" || res.target.Bucket != "new" {
		t.Fatalf("run target = %+v, want the post-edit destination", res.target)
	}
}

// TestDestinationEditWaitsForRunLock is the U3 regression, run-first
// direction: while a run holds the target row, the destination edit blocks;
// once the run commits, the edit sees the running backup and is refused.
func TestDestinationEditWaitsForRunLock(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	target, database := seedRaceTarget(t, st, repo, ownerID, serverID)

	runLocked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRun := func() { releaseOnce.Do(func() { close(release) }) }
	st.BeforeBackupRunCommit = func() {
		close(runLocked)
		<-release
	}
	// LIFO: release the run before clearing the seam, so a t.Fatalf before the
	// explicit release cannot leave the run goroutine blocking the pool close.
	t.Cleanup(func() { st.BeforeBackupRunCommit = nil })
	t.Cleanup(releaseRun)

	runDone := make(chan error, 1)
	go func() {
		_, _, err := repo.CreateBackupWithTarget(ctx, Backup{
			ID: uuid.New(), DatabaseID: database.ID, TargetID: target.ID,
			Type: BackupManual, Status: BackupRunning,
		}, nil)
		runDone <- err
	}()
	<-runLocked

	editDone := make(chan error, 1)
	go func() {
		_, err := repo.UpdateBackupTargetWithSecrets(ctx, BackupTarget{
			ID: target.ID, Name: target.Name, Kind: TargetS3,
			Endpoint: "https://new", Bucket: "new",
		}, nil)
		editDone <- err
	}()

	select {
	case err := <-editDone:
		t.Fatalf("destination edit did not wait for the run lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	releaseRun()
	if err := <-runDone; err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := <-editDone; !errors.Is(err, ErrTargetStranded) {
		t.Fatalf("edit err = %v, want ErrTargetStranded once the run exists", err)
	}
	assertTargetState(t, repo, ctx, target.ID, target.Name, "https://old", "old")
}

// TestBackupTargetValidationLeavesNoRow is the store-level half of the round-2
// U1 fix: the ownership validation runs on the locked target before the run is
// inserted, so a rejected target creates no running row.
func TestBackupTargetValidationLeavesNoRow(t *testing.T) {
	_, st := integrationEnv(t)
	repo := newStoreBackupRepository(st)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	target, database := seedRaceTarget(t, st, repo, ownerID, serverID)

	before, err := repo.ListBackupsByDatabase(ctx, database.ID, maxBackupListLimit)
	if err != nil {
		t.Fatalf("ListBackupsByDatabase: %v", err)
	}
	_, _, err = repo.CreateBackupWithTarget(ctx, Backup{
		ID: uuid.New(), DatabaseID: database.ID, TargetID: target.ID,
		Type: BackupManual, Status: BackupRunning,
	}, func(*BackupTarget) error { return ErrNotFound })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateBackupWithTarget err = %v, want the validation error", err)
	}
	after, err := repo.ListBackupsByDatabase(ctx, database.ID, maxBackupListLimit)
	if err != nil {
		t.Fatalf("ListBackupsByDatabase: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("rejected validation left %d rows, want %d", len(after), len(before))
	}
}
